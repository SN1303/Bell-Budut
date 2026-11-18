package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"bell-budut/audio"
	"bell-budut/database"
	"bell-budut/excel"
	"bell-budut/scheduler"
)

type Server struct {
	db          *database.DB
	audioMgr    *audio.Manager
	sched       *scheduler.Scheduler
	audioDir    string
	sessionsMu  sync.RWMutex
	sessions    map[string]time.Time
}

func NewServer(db *database.DB, audioMgr *audio.Manager, sched *scheduler.Scheduler, audioDir string) *Server {
	s := &Server{
		db:       db,
		audioMgr: audioMgr,
		sched:    sched,
		audioDir: audioDir,
		sessions: make(map[string]time.Time),
	}

	// Auto-broadcast when server soundcard finishes playing naturally
	s.audioMgr.SetOnEnd(func(code, name string) {
		stopPayload := map[string]interface{}{
			"type":       "SERVER_AUDIO_STOPPED",
			"audio_code": code,
			"audio_name": name,
		}
		data, _ := json.Marshal(stopPayload)
		s.sched.Broadcast(data)
	})

	return s
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Static Assets
	staticDir := filepath.Join("web", "static")
	fileServer := http.FileServer(http.Dir(staticDir))
	mux.Handle("/static/", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		fileServer.ServeHTTP(w, r)
	})))

	// Pages
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/kiosk", s.handleKiosk)

	// SSE Real-time stream
	mux.HandleFunc("/api/events", s.handleSSE)

	// Auth PIN
	mux.HandleFunc("/api/verify-pin", s.handleVerifyPIN)
	mux.HandleFunc("/api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/change-pin", s.requireAuth(s.handleChangePIN))

	// Audio Streaming & Management
	mux.HandleFunc("/api/audio/stream/", s.handleAudioStream)
	mux.HandleFunc("/api/audio/folder/", s.handleAudioFolder)
	mux.HandleFunc("/api/audio", s.handleAudioList)
	mux.HandleFunc("/api/audio/upload", s.requireAuth(s.handleAudioUpload))
	mux.HandleFunc("/api/audio/delete/", s.requireAuth(s.handleAudioDelete))
	mux.HandleFunc("/api/audio/test-server", s.handleAudioTestServer)
	mux.HandleFunc("/api/audio/stop-server", s.handleAudioStopServer)

	// Groups
	mux.HandleFunc("/api/groups", s.handleGroups)
	mux.HandleFunc("/api/groups/create", s.requireAuth(s.handleGroupCreate))
	mux.HandleFunc("/api/groups/update/", s.requireAuth(s.handleGroupUpdate))
	mux.HandleFunc("/api/groups/delete/", s.requireAuth(s.handleGroupDelete))
	mux.HandleFunc("/api/groups/activate/", s.requireAuth(s.handleGroupActivate))
	mux.HandleFunc("/api/groups/date-range/", s.requireAuth(s.handleGroupDateRange))

	// Schedules
	mux.HandleFunc("/api/schedules", s.handleSchedules)
	mux.HandleFunc("/api/schedules/create", s.requireAuth(s.handleScheduleCreate))
	mux.HandleFunc("/api/schedules/update/", s.requireAuth(s.handleScheduleUpdate))
	mux.HandleFunc("/api/schedules/delete/", s.requireAuth(s.handleScheduleDelete))
	mux.HandleFunc("/api/schedules/toggle/", s.requireAuth(s.handleScheduleToggle))

	// Triggers & Controls
	mux.HandleFunc("/api/trigger-manual", s.requireAuth(s.handleTriggerManual))
	mux.HandleFunc("/api/toggle-holiday", s.requireAuth(s.handleToggleHoliday))
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/autostart", s.handleAutoStart)

	// System Audio Isolator & Windows Settings Helpers
	mux.HandleFunc("/api/system/open-volume-mixer", s.handleOpenVolumeMixer)
	mux.HandleFunc("/api/system/open-sndvol", s.handleOpenSndvol)
	mux.HandleFunc("/api/system/windows-sounds", s.handleWindowsSounds)

	// Excel
	mux.HandleFunc("/api/excel/template", s.handleExcelTemplate)
	mux.HandleFunc("/api/excel/template.xlsx", s.handleExcelTemplate)
	mux.HandleFunc("/api/excel/export", s.handleExcelExport)
	mux.HandleFunc("/api/excel/export.xlsx", s.handleExcelExport)
	mux.HandleFunc("/api/excel/import", s.requireAuth(s.handleExcelImport))

	// Backup & Restore
	mux.HandleFunc("/api/backup/download", s.requireAuth(s.handleBackupDownload))
	mux.HandleFunc("/api/backup/restore", s.requireAuth(s.handleBackupRestore))

	// Logs
	mux.HandleFunc("/api/logs", s.handleLogs)

	return mux
}

// ----------------- AUTH HELPERS -----------------

func (s *Server) createSession() string {
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)
	exp := time.Now().Add(7 * 24 * time.Hour) // 7 days session
	s.sessionsMu.Lock()
	s.sessions[token] = exp
	s.sessionsMu.Unlock()
	_ = s.db.CreateSession(token, exp)
	return token
}

func (s *Server) isValidSession(token string) bool {
	if token == "" {
		return false
	}
	s.sessionsMu.RLock()
	exp, ok := s.sessions[token]
	s.sessionsMu.RUnlock()
	if ok && time.Now().Before(exp) {
		return true
	}
	// Fallback to SQLite DB
	if s.db.IsValidSession(token) {
		s.sessionsMu.Lock()
		s.sessions[token] = time.Now().Add(7 * 24 * time.Hour)
		s.sessionsMu.Unlock()
		return true
	}
	return false
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("bell_auth")
		var token string
		if err == nil {
			token = cookie.Value
		}
		if token == "" {
			token = r.Header.Get("X-Admin-Token")
		}

		if !s.isValidSession(token) {
			_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 10<<20))
			s.jsonError(w, "Diperlukan autentikasi PIN Admin", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) checkAuth(r *http.Request) bool {
	cookie, err := r.Cookie("bell_auth")
	var token string
	if err == nil {
		token = cookie.Value
	}
	if token == "" {
		token = r.Header.Get("X-Admin-Token")
	}
	return s.isValidSession(token)
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("bell_auth")
	var token string
	if err == nil {
		token = cookie.Value
	}
	if token == "" {
		token = r.Header.Get("X-Admin-Token")
	}

	valid := s.isValidSession(token)
	s.jsonSuccess(w, map[string]interface{}{
		"is_admin": valid,
	})
}

func (s *Server) handleVerifyPIN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		PIN string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Data tidak valid", http.StatusBadRequest)
		return
	}

	actualPIN := s.db.GetSetting("admin_pin", "1234")
	if req.PIN != actualPIN {
		s.jsonError(w, "PIN Admin salah", http.StatusForbidden)
		return
	}

	token := s.createSession()
	http.SetCookie(w, &http.Cookie{
		Name:     "bell_auth",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   12 * 3600,
	})

	s.jsonSuccess(w, map[string]interface{}{
		"token":   token,
		"message": "Login Admin berhasil",
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("bell_auth")
	var token string
	if err == nil && cookie.Value != "" {
		token = cookie.Value
	}
	if token == "" {
		token = r.Header.Get("X-Admin-Token")
	}
	if token != "" {
		s.sessionsMu.Lock()
		delete(s.sessions, token)
		s.sessionsMu.Unlock()
		_ = s.db.DeleteSession(token)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "bell_auth",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	s.jsonSuccess(w, "Logout berhasil")
}

func (s *Server) handleChangePIN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		OldPIN string `json:"old_pin"`
		NewPIN string `json:"new_pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Data tidak valid", http.StatusBadRequest)
		return
	}

	actualPIN := s.db.GetSetting("admin_pin", "1234")
	if req.OldPIN != actualPIN {
		s.jsonError(w, "PIN Lama tidak sesuai", http.StatusBadRequest)
		return
	}
	if len(req.NewPIN) < 4 {
		s.jsonError(w, "PIN baru minimal 4 karakter", http.StatusBadRequest)
		return
	}

	_ = s.db.SetSetting("admin_pin", req.NewPIN)
	s.jsonSuccess(w, "PIN Admin berhasil diperbarui")
}

// ----------------- PAGES -----------------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	tmplPath := filepath.Join("web", "templates", "index.html")
	http.ServeFile(w, r, tmplPath)
}

func (s *Server) handleKiosk(w http.ResponseWriter, r *http.Request) {
	tmplPath := filepath.Join("web", "templates", "kiosk.html")
	http.ServeFile(w, r, tmplPath)
}

// ----------------- SSE EVENT STREAM -----------------

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	clientChan := s.sched.Subscribe()
	defer s.sched.Unsubscribe(clientChan)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-clientChan:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

// ----------------- AUDIO HANDLERS -----------------

func (s *Server) handleAudioList(w http.ResponseWriter, r *http.Request) {
	audios, err := s.db.GetAudioFiles()
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonSuccess(w, audios)
}

func (s *Server) handleAudioStream(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimPrefix(r.URL.Path, "/api/audio/stream/")
	audioFile, err := s.db.GetAudioByCode(code)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	rawPath := filepath.Join(s.audioDir, audioFile.Filename)
	var resolvedPath string
	specificFile := strings.TrimSpace(r.URL.Query().Get("file"))
	if specificFile != "" && audioFile.IsFolder {
		cleanName := filepath.Base(specificFile)
		resolvedPath = filepath.Join(rawPath, cleanName)
	} else {
		resolvedPath, _, err = s.audioMgr.ResolvePlayPath(rawPath, audioFile.Name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}

	f, err := os.Open(resolvedPath)
	if err != nil {
		http.Error(w, "Gagal membuka file audio", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	info, _ := f.Stat()
	ext := strings.ToLower(filepath.Ext(resolvedPath))
	contentType := "audio/wav"
	if ext == ".mp3" {
		contentType = "audio/mpeg"
	} else if ext == ".ogg" {
		contentType = "audio/ogg"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, filepath.Base(resolvedPath), info.ModTime(), f)
}

func (s *Server) handleAudioUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}

	// Max 500MB for batch uploads
	if err := r.ParseMultipartForm(500 << 20); err != nil {
		s.jsonError(w, "Ukuran file terlalu besar atau form tidak valid", http.StatusBadRequest)
		return
	}

	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		s.jsonError(w, "Data form tidak ditemukan", http.StatusBadRequest)
		return
	}

	// Check files under "audio_files" or "audio_file"
	fileHeaders := r.MultipartForm.File["audio_files"]
	if len(fileHeaders) == 0 {
		fileHeaders = r.MultipartForm.File["audio_file"]
	}

	if len(fileHeaders) == 0 {
		s.jsonError(w, "Tidak ada file audio yang dipilih", http.StatusBadRequest)
		return
	}

	customName := strings.TrimSpace(r.FormValue("name"))
	customDesc := strings.TrimSpace(r.FormValue("description"))

	targetFolderCode := strings.TrimSpace(r.FormValue("target_folder_code"))
	asFolder := r.FormValue("as_folder") == "true" || r.FormValue("as_folder") == "on"
	folderName := strings.TrimSpace(r.FormValue("folder_name"))

	// Option A1: Add into an EXISTING Folder Playlist (e.g. FLD-01)
	if targetFolderCode != "" {
		folderAudio, err := s.db.GetAudioByCode(targetFolderCode)
		if err != nil || !folderAudio.IsFolder {
			s.jsonError(w, "Folder audio tujuan tidak ditemukan", http.StatusBadRequest)
			return
		}
		folderAbs := filepath.Join(s.audioDir, folderAudio.Filename)
		if err := os.MkdirAll(folderAbs, 0755); err != nil {
			s.jsonError(w, "Gagal mengakses folder di server: "+err.Error(), http.StatusInternalServerError)
			return
		}

		savedCount := 0
		for _, header := range fileHeaders {
			ext := strings.ToLower(filepath.Ext(header.Filename))
			if ext != ".mp3" && ext != ".wav" && ext != ".ogg" {
				continue
			}
			file, err := header.Open()
			if err != nil {
				continue
			}
			cleanName := filepath.Base(header.Filename)
			targetFile := filepath.Join(folderAbs, cleanName)
			out, err := os.Create(targetFile)
			if err != nil {
				file.Close()
				continue
			}
			_, copyErr := io.Copy(out, file)
			out.Close()
			file.Close()
			if copyErr == nil {
				savedCount++
			}
		}

		if savedCount == 0 {
			s.jsonError(w, "Tidak ada file audio valid yang berhasil disimpan ke dalam folder", http.StatusBadRequest)
			return
		}

		totalFiles := 0
		if entries, err := os.ReadDir(folderAbs); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					ext := strings.ToLower(filepath.Ext(e.Name()))
					if ext == ".mp3" || ext == ".wav" || ext == ".ogg" {
						totalFiles++
					}
				}
			}
		}

		s.jsonSuccess(w, map[string]interface{}{
			"count":       savedCount,
			"total_count": totalFiles,
			"code":        folderAudio.Code,
			"is_folder":   true,
			"messages":    fmt.Sprintf("Berhasil menambahkan %d lagu ke %s (Total: %d lagu)", savedCount, folderAudio.Name, totalFiles),
		})
		return
	}

	// Option A2: Save as a NEW Random Shuffle Folder (FLD-XX)
	if asFolder {
		if folderName == "" {
			folderName = customName
		}
		if folderName == "" {
			folderName = fmt.Sprintf("Folder Lagu Acak (%s)", time.Now().Format("02/01/2006"))
		}

		folderRel := fmt.Sprintf("fld_%d", time.Now().Unix())
		folderAbs := filepath.Join(s.audioDir, folderRel)
		if err := os.MkdirAll(folderAbs, 0755); err != nil {
			s.jsonError(w, "Gagal membuat folder di server: "+err.Error(), http.StatusInternalServerError)
			return
		}

		savedCount := 0
		for _, header := range fileHeaders {
			ext := strings.ToLower(filepath.Ext(header.Filename))
			if ext != ".mp3" && ext != ".wav" && ext != ".ogg" {
				continue
			}
			file, err := header.Open()
			if err != nil {
				continue
			}

			cleanName := filepath.Base(header.Filename)
			targetFile := filepath.Join(folderAbs, cleanName)
			out, err := os.Create(targetFile)
			if err != nil {
				file.Close()
				continue
			}
			_, copyErr := io.Copy(out, file)
			out.Close()
			file.Close()
			if copyErr == nil {
				savedCount++
			}
		}

		if savedCount == 0 {
			os.RemoveAll(folderAbs)
			s.jsonError(w, "Tidak ada file audio valid yang berhasil disimpan ke dalam folder", http.StatusBadRequest)
			return
		}

		desc := customDesc
		if desc == "" {
			desc = fmt.Sprintf("Folder acak berisi %d lagu", savedCount)
		}

		audioRecord, err := s.db.AddAudioFile(folderName, folderRel, desc, 0, true)
		if err != nil {
			os.RemoveAll(folderAbs)
			s.jsonError(w, "Gagal mencatat folder audio di database: "+err.Error(), http.StatusInternalServerError)
			return
		}

		s.jsonSuccess(w, map[string]interface{}{
			"count":     savedCount,
			"code":      audioRecord.Code,
			"is_folder": true,
			"records":   []*database.AudioFile{audioRecord},
			"messages":  fmt.Sprintf("Berhasil membuat folder acak [%s] (%s) berisi %d lagu", folderName, audioRecord.Code, savedCount),
		})
		return
	}

	// Option B: Save as Individual Audio Files (AUD-XX)
	var uploadedRecords []*database.AudioFile
	var errorMessages []string

	for i, header := range fileHeaders {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext != ".mp3" && ext != ".wav" && ext != ".ogg" {
			errorMessages = append(errorMessages, fmt.Sprintf("%s: Format harus MP3, WAV, atau OGG", header.Filename))
			continue
		}

		file, err := header.Open()
		if err != nil {
			errorMessages = append(errorMessages, fmt.Sprintf("%s: Gagal membuka file", header.Filename))
			continue
		}

		// Determine song title
		name := strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
		if len(fileHeaders) == 1 && customName != "" {
			name = customName
		}
		desc := customDesc
		if desc == "" && len(fileHeaders) > 1 {
			desc = "Diunggah sekaligus (" + time.Now().Format("02/01/2006") + ")"
		}

		// Safe filename with nano timestamp + index to prevent any collisions
		safeFilename := fmt.Sprintf("%d_%d_audio%s", time.Now().UnixNano(), i, ext)
		targetPath := filepath.Join(s.audioDir, safeFilename)

		out, err := os.Create(targetPath)
		if err != nil {
			file.Close()
			errorMessages = append(errorMessages, fmt.Sprintf("%s: Gagal menyimpan di server", header.Filename))
			continue
		}

		_, copyErr := io.Copy(out, file)
		out.Close()
		file.Close()

		if copyErr != nil {
			os.Remove(targetPath)
			errorMessages = append(errorMessages, fmt.Sprintf("%s: Gagal menulis data", header.Filename))
			continue
		}

		audioRecord, err := s.db.AddAudioFile(name, safeFilename, desc, 0, false)
		if err != nil {
			os.Remove(targetPath)
			errorMessages = append(errorMessages, fmt.Sprintf("%s: Gagal menyimpan di database", header.Filename))
			continue
		}

		uploadedRecords = append(uploadedRecords, audioRecord)
	}

	if len(uploadedRecords) == 0 {
		s.jsonError(w, "Gagal mengunggah file: "+strings.Join(errorMessages, ", "), http.StatusBadRequest)
		return
	}

	firstCode := ""
	if len(uploadedRecords) > 0 {
		firstCode = uploadedRecords[0].Code
	}

	s.jsonSuccess(w, map[string]interface{}{
		"count":    len(uploadedRecords),
		"records":  uploadedRecords,
		"code":     firstCode, // backward compatibility
		"errors":   errorMessages,
		"messages": fmt.Sprintf("Berhasil mengunggah %d file audio", len(uploadedRecords)),
	})
}

func (s *Server) handleAudioDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/api/audio/delete/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, "ID audio tidak valid", http.StatusBadRequest)
		return
	}

	deletedAudio, err := s.db.DeleteAudioFile(id)
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Remove physical file or directory if not builtin
	if deletedAudio != nil && !deletedAudio.IsBuiltin {
		targetPath := filepath.Join(s.audioDir, deletedAudio.Filename)
		if deletedAudio.IsFolder {
			_ = os.RemoveAll(targetPath)
		} else {
			_ = os.Remove(targetPath)
		}
	}

	s.jsonSuccess(w, "Audio berhasil dihapus")
}

type FolderFileInfo struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	SizeBytes int64  `json:"size_bytes"`
	SizeFmt   string `json:"size_fmt"`
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func (s *Server) handleAudioFolder(w http.ResponseWriter, r *http.Request) {
	// Path format: /api/audio/folder/{code} or /api/audio/folder/{code}/file
	subPath := strings.TrimPrefix(r.URL.Path, "/api/audio/folder/")
	parts := strings.Split(strings.Trim(subPath, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		s.jsonError(w, "Kode folder tidak valid", http.StatusBadRequest)
		return
	}

	code := parts[0]
	folderAudio, err := s.db.GetAudioByCode(code)
	if err != nil || !folderAudio.IsFolder {
		s.jsonError(w, "Folder audio tidak ditemukan", http.StatusNotFound)
		return
	}

	folderAbs := filepath.Join(s.audioDir, folderAudio.Filename)

	// DELETE /api/audio/folder/{code}/file?name=xxx
	if len(parts) >= 2 && parts[1] == "file" && (r.Method == http.MethodDelete || r.Method == http.MethodPost) {
		if !s.checkAuth(r) {
			s.jsonError(w, "Perlu PIN Admin", http.StatusUnauthorized)
			return
		}
		fileName := strings.TrimSpace(r.URL.Query().Get("name"))
		if fileName == "" {
			s.jsonError(w, "Nama file tidak valid", http.StatusBadRequest)
			return
		}
		cleanName := filepath.Base(fileName)
		targetPath := filepath.Join(folderAbs, cleanName)
		if err := os.Remove(targetPath); err != nil {
			s.jsonError(w, "Gagal menghapus file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonSuccess(w, fmt.Sprintf("Lagu '%s' berhasil dihapus dari folder", cleanName))
		return
	}

	// GET /api/audio/folder/{code} -> List songs in folder
	if r.Method == http.MethodGet {
		entries, err := os.ReadDir(folderAbs)
		if err != nil {
			s.jsonError(w, "Gagal membaca folder: "+err.Error(), http.StatusInternalServerError)
			return
		}

		var files []FolderFileInfo
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".mp3" && ext != ".wav" && ext != ".ogg" {
				continue
			}
			info, err := e.Info()
			size := int64(0)
			if err == nil {
				size = info.Size()
			}
			cleanTitle := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			files = append(files, FolderFileInfo{
				Name:      e.Name(),
				Title:     cleanTitle,
				SizeBytes: size,
				SizeFmt:   formatBytes(size),
			})
		}

		s.jsonSuccess(w, map[string]interface{}{
			"code":        folderAudio.Code,
			"name":        folderAudio.Name,
			"folder_name": folderAudio.Filename,
			"description": folderAudio.Description,
			"count":       len(files),
			"files":       files,
		})
		return
	}

	s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
}

func (s *Server) handleAudioTestServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Data tidak valid", http.StatusBadRequest)
		return
	}

	audioFile, err := s.db.GetAudioByCode(req.Code)
	if err != nil {
		s.jsonError(w, "Audio tidak ditemukan", http.StatusNotFound)
		return
	}

	filePath := filepath.Join(s.audioDir, audioFile.Filename)
	if err := s.audioMgr.PlayFile(filePath, audioFile.Code, audioFile.Name); err != nil {
		s.jsonError(w, "Gagal memutar di speaker server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Broadcast that server audio started
	startPayload := map[string]interface{}{
		"type":       "SERVER_AUDIO_STARTED",
		"audio_code": audioFile.Code,
		"audio_name": audioFile.Name,
	}
	data, _ := json.Marshal(startPayload)
	s.sched.Broadcast(data)

	s.jsonSuccess(w, fmt.Sprintf("Sedang memutar '%s' di speaker server", audioFile.Name))
}

func (s *Server) handleAudioStopServer(w http.ResponseWriter, r *http.Request) {
	if err := s.audioMgr.Stop(); err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Broadcast that server audio stopped
	stopPayload := map[string]interface{}{
		"type": "SERVER_AUDIO_STOPPED",
	}
	data, _ := json.Marshal(stopPayload)
	s.sched.Broadcast(data)

	s.jsonSuccess(w, "Audio di server dihentikan")
}

// ----------------- GROUPS HANDLERS -----------------

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.db.GetGroups()
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonSuccess(w, groups)
}

func (s *Server) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Data tidak valid", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		s.jsonError(w, "Nama grup tidak boleh kosong", http.StatusBadRequest)
		return
	}

	g, err := s.db.CreateGroup(req.Name, req.Description)
	if err != nil {
		s.jsonError(w, "Gagal membuat grup: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.jsonSuccess(w, g)
}

func (s *Server) handleGroupUpdate(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/groups/update/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, "ID grup tidak valid", http.StatusBadRequest)
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Data tidak valid", http.StatusBadRequest)
		return
	}

	if err := s.db.UpdateGroup(id, req.Name, req.Description); err != nil {
		s.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.jsonSuccess(w, "Grup berhasil diperbarui")
}

func (s *Server) handleGroupDelete(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/groups/delete/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, "ID grup tidak valid", http.StatusBadRequest)
		return
	}

	if err := s.db.DeleteGroup(id); err != nil {
		s.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.jsonSuccess(w, "Grup berhasil dihapus")
}

func (s *Server) handleGroupActivate(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/groups/activate/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, "ID grup tidak valid", http.StatusBadRequest)
		return
	}

	if err := s.db.SetActiveGroup(id); err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonSuccess(w, "Grup aktif berhasil diubah")
}

func (s *Server) handleGroupDateRange(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/groups/date-range/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, "ID grup tidak valid", http.StatusBadRequest)
		return
	}

	var req struct {
		Enabled   bool   `json:"enabled"`
		StartDate string `json:"start_date"` // YYYY-MM-DD
		EndDate   string `json:"end_date"`   // YYYY-MM-DD
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Data tidak valid", http.StatusBadRequest)
		return
	}

	if err := s.db.SetGroupDateRange(id, req.Enabled, req.StartDate, req.EndDate); err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonSuccess(w, "Rentang tanggal jadwal berhasil disimpan")
}

// ----------------- SCHEDULES HANDLERS -----------------

func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	groupIDStr := r.URL.Query().Get("group_id")
	day := r.URL.Query().Get("day")

	var groupID int64
	if groupIDStr != "" {
		groupID, _ = strconv.ParseInt(groupIDStr, 10, 64)
	}

	list, err := s.db.GetSchedules(groupID, day)
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonSuccess(w, list)
}

func (s *Server) handleScheduleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}
	var sched database.Schedule
	if err := json.NewDecoder(r.Body).Decode(&sched); err != nil {
		s.jsonError(w, "Data tidak valid", http.StatusBadRequest)
		return
	}

	if sched.GroupID <= 0 || sched.Time == "" || sched.Title == "" || sched.AudioCode == "" {
		s.jsonError(w, "Grup, Waktu, Judul, dan Kode Audio wajib diisi", http.StatusBadRequest)
		return
	}

	created, err := s.db.CreateSchedule(&sched)
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonSuccess(w, created)
}

func (s *Server) handleScheduleUpdate(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/schedules/update/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, "ID jadwal tidak valid", http.StatusBadRequest)
		return
	}

	var sched database.Schedule
	if err := json.NewDecoder(r.Body).Decode(&sched); err != nil {
		s.jsonError(w, "Data tidak valid", http.StatusBadRequest)
		return
	}
	sched.ID = id

	if err := s.db.UpdateSchedule(&sched); err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonSuccess(w, "Jadwal berhasil diperbarui")
}

func (s *Server) handleScheduleDelete(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/schedules/delete/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, "ID jadwal tidak valid", http.StatusBadRequest)
		return
	}

	if err := s.db.DeleteSchedule(id); err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonSuccess(w, "Jadwal berhasil dihapus")
}

func (s *Server) handleScheduleToggle(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/schedules/toggle/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, "ID jadwal tidak valid", http.StatusBadRequest)
		return
	}

	newState, err := s.db.ToggleSchedule(id)
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonSuccess(w, map[string]interface{}{"is_enabled": newState})
}

// ----------------- TRIGGERS & CONTROLS -----------------

func (s *Server) handleTriggerManual(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AudioCode   string `json:"audio_code"`
		TriggerType string `json:"trigger_type"` // MANUAL or EMERGENCY
		CustomTitle string `json:"custom_title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Data tidak valid", http.StatusBadRequest)
		return
	}

	if req.TriggerType == "" {
		req.TriggerType = "MANUAL"
	}

	if err := s.sched.TriggerManual(req.AudioCode, req.TriggerType, req.CustomTitle); err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonSuccess(w, "Bel berhasil dibunyikan")
}

func (s *Server) handleToggleHoliday(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}
	cur := s.db.GetSetting("holiday_mode", "false")
	newVal := "true"
	if cur == "true" {
		newVal = "false"
	}
	_ = s.db.SetSetting("holiday_mode", newVal)
	s.jsonSuccess(w, map[string]interface{}{"holiday_mode": newVal == "true"})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		settings, err := s.db.GetAllSettings()
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Do not leak admin PIN in plain settings
		delete(settings, "admin_pin")
		s.jsonSuccess(w, settings)
		return
	}

	if r.Method == http.MethodPost {
		cookie, err := r.Cookie("bell_auth")
		var token string
		if err == nil {
			token = cookie.Value
		}
		if !s.isValidSession(token) {
			s.jsonError(w, "Diperlukan PIN Admin", http.StatusUnauthorized)
			return
		}

		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, "Data tidak valid", http.StatusBadRequest)
			return
		}

		for k, v := range req {
			if k != "admin_pin" {
				_ = s.db.SetSetting(k, v)
			}
		}
		s.jsonSuccess(w, "Pengaturan disimpan")
		return
	}
}

func (s *Server) handleAutoStart(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS != "windows" {
		s.jsonSuccess(w, map[string]interface{}{"enabled": false, "supported": false})
		return
	}

	if r.Method == http.MethodGet {
		cmd := exec.Command("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "BellBudut")
		err := cmd.Run()
		s.jsonSuccess(w, map[string]interface{}{
			"enabled":   err == nil,
			"supported": true,
		})
		return
	}

	if r.Method == http.MethodPost {
		cookie, err := r.Cookie("bell_auth")
		var token string
		if err == nil {
			token = cookie.Value
		}
		if token == "" {
			token = r.Header.Get("X-Admin-Token")
		}
		if !s.isValidSession(token) {
			s.jsonError(w, "Diperlukan PIN Admin", http.StatusUnauthorized)
			return
		}

		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, "Data tidak valid", http.StatusBadRequest)
			return
		}

		if req.Enabled {
			exePath, err := os.Executable()
			if err != nil {
				s.jsonError(w, "Gagal mendapatkan lokasi aplikasi: "+err.Error(), http.StatusInternalServerError)
				return
			}
			dir := filepath.Dir(exePath)
			vbsPath := filepath.Join(dir, "Jalankan_Background.vbs")
			val := fmt.Sprintf(`wscript.exe "%s"`, vbsPath)

			cmd := exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "BellBudut", "/t", "REG_SZ", "/d", val, "/f")
			if out, err := cmd.CombinedOutput(); err != nil {
				s.jsonError(w, "Gagal mendaftarkan auto-start: "+string(out), http.StatusInternalServerError)
				return
			}
			s.jsonSuccess(w, map[string]interface{}{"enabled": true, "message": "Auto-Start Windows berhasil diaktifkan"})
		} else {
			cmd := exec.Command("reg", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "BellBudut", "/f")
			_ = cmd.Run()
			s.jsonSuccess(w, map[string]interface{}{"enabled": false, "message": "Auto-Start Windows berhasil dinonaktifkan"})
		}
		return
	}

	s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
}

func (s *Server) handleOpenVolumeMixer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}
	cmd := exec.Command("cmd", "/c", "start", "ms-settings:apps-volume")
	if err := cmd.Start(); err != nil {
		_ = exec.Command("sndvol.exe").Start()
	}
	s.jsonSuccess(w, "Pengatur Suara Aplikasi Windows berhasil dibuka di layar monitor")
}

func (s *Server) handleOpenSndvol(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}
	cmd := exec.Command("sndvol.exe")
	if err := cmd.Start(); err != nil {
		s.jsonError(w, "Gagal membuka Volume Mixer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonSuccess(w, "Volume Mixer Windows klasik berhasil dibuka di layar monitor")
}

func (s *Server) handleWindowsSounds(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS != "windows" {
		s.jsonSuccess(w, map[string]interface{}{"is_muted": false, "supported": false})
		return
	}

	if r.Method == http.MethodGet {
		out, err := exec.Command("reg", "query", `HKCU\AppEvents\Schemes`, "/ve").Output()
		isMuted := err == nil && strings.Contains(string(out), ".None")
		s.jsonSuccess(w, map[string]interface{}{
			"is_muted":  isMuted,
			"supported": true,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Mute bool `json:"mute"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, "Data tidak valid", http.StatusBadRequest)
			return
		}

		targetVal := ".None"
		msg := "Bunyi notifikasi & suara sistem Windows berhasil dibisukan"
		if !req.Mute {
			targetVal = ".Default"
			msg = "Bunyi notifikasi Windows dikembalikan ke default"
		}

		cmd := exec.Command("reg", "add", `HKCU\AppEvents\Schemes`, "/ve", "/t", "REG_SZ", "/d", targetVal, "/f")
		if err := cmd.Run(); err != nil {
			s.jsonError(w, "Gagal mengubah skema suara Windows: "+err.Error(), http.StatusInternalServerError)
			return
		}

		s.jsonSuccess(w, map[string]interface{}{"is_muted": req.Mute, "message": msg})
		return
	}

	s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
}

// ----------------- EXCEL HANDLERS -----------------

func (s *Server) handleExcelTemplate(w http.ResponseWriter, r *http.Request) {
	audios, _ := s.db.GetAudioFiles()
	groups, _ := s.db.GetGroups()

	targetGroup := strings.TrimSpace(r.URL.Query().Get("group_name"))
	if targetGroup == "" {
		groupIDStr := r.URL.Query().Get("group_id")
		if groupIDStr != "" && groupIDStr != "all" {
			if gID, err := strconv.ParseInt(groupIDStr, 10, 64); err == nil && gID > 0 {
				for _, g := range groups {
					if g.ID == gID {
						targetGroup = g.Name
						break
					}
				}
			}
		}
	}

	data, err := excel.GenerateTemplate(audios, groups, targetGroup)
	if err != nil {
		s.jsonError(w, "Gagal membuat template Excel: "+err.Error(), http.StatusInternalServerError)
		return
	}

	filename := "Template_Jadwal_Bel_Budut.xlsx"
	if targetGroup != "" {
		clean := strings.ReplaceAll(targetGroup, " ", "_")
		filename = fmt.Sprintf("Template_Jadwal_%s.xlsx", clean)
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(data)
}

func (s *Server) handleExcelExport(w http.ResponseWriter, r *http.Request) {
	audios, _ := s.db.GetAudioFiles()
	groups, _ := s.db.GetGroups()

	var targetGroupID int64
	var targetGroupName string

	groupIDStr := r.URL.Query().Get("group_id")
	if groupIDStr != "" && groupIDStr != "all" {
		targetGroupID, _ = strconv.ParseInt(groupIDStr, 10, 64)
	}

	for _, g := range groups {
		if g.ID == targetGroupID {
			targetGroupName = g.Name
			break
		}
	}

	schedules, _ := s.db.GetSchedules(targetGroupID, "")

	data, err := excel.ExportSchedules(schedules, audios, groups, targetGroupName)
	if err != nil {
		s.jsonError(w, "Gagal ekspor jadwal ke Excel: "+err.Error(), http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("Jadwal_Semua_Grup_%s.xlsx", time.Now().Format("20060102"))
	if targetGroupName != "" {
		clean := strings.ReplaceAll(targetGroupName, " ", "_")
		filename = fmt.Sprintf("Jadwal_%s_%s.xlsx", clean, time.Now().Format("20060102"))
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(data)
}

func (s *Server) handleExcelImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}

	_ = r.ParseMultipartForm(10 << 20)
	file, _, err := r.FormFile("excel_file")
	if err != nil {
		s.jsonError(w, "File Excel tidak ditemukan dalam request", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		s.jsonError(w, "Gagal membaca isi file Excel", http.StatusBadRequest)
		return
	}

	groupMode := r.FormValue("group_mode") // "specific", "new", or "file"
	if groupMode == "" {
		groupMode = "file"
	}

	targetGroupID, _ := strconv.ParseInt(r.FormValue("target_group_id"), 10, 64)
	newGroupName := r.FormValue("new_group_name")
	mode := r.FormValue("mode")
	replaceAll := mode == "replace"

	report, err := excel.ImportSchedules(s.db, data, groupMode, targetGroupID, newGroupName, replaceAll)
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.jsonSuccess(w, report)
}

// ----------------- BACKUP & RESTORE -----------------

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	filename := fmt.Sprintf("backup_bell_budut_%s.db", time.Now().Format("20060102_150405"))
	w.Header().Set("Content-Type", "application/x-sqlite3")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))

	if err := s.db.BackupDatabase(w); err != nil {
		// Can't jsonError if header already sent
		return
	}
}

func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}

	_ = r.ParseMultipartForm(50 << 20)
	file, _, err := r.FormFile("db_file")
	if err != nil {
		s.jsonError(w, "File database (.db) tidak ditemukan", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if err := s.db.RestoreDatabase(file); err != nil {
		s.jsonError(w, "Gagal restore database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonSuccess(w, "Database berhasil dipulihkan (restore)")
}

// ----------------- LOGS -----------------

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	logs, err := s.db.GetRecentLogs(limit)
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonSuccess(w, logs)
}

// ----------------- JSON RESPONDERS -----------------

func (s *Server) jsonSuccess(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    data,
	})
}

func (s *Server) jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   msg,
	})
}

