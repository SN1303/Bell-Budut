package database

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

type Group struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	IsDefault        bool   `json:"is_default"`
	IsActive         bool   `json:"is_active"`
	DateRangeEnabled bool   `json:"date_range_enabled"`
	StartDate        string `json:"start_date"` // YYYY-MM-DD
	EndDate          string `json:"end_date"`   // YYYY-MM-DD
	CreatedAt        string `json:"created_at"`
	ScheduleCount    int    `json:"schedule_count"`
}

type AudioFile struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Filename    string `json:"filename"`
	DurationSec int    `json:"duration_sec"`
	Description string `json:"description"`
	IsBuiltin   bool   `json:"is_builtin"`
	IsFolder    bool   `json:"is_folder"`
	CreatedAt   string `json:"created_at"`
}

type Schedule struct {
	ID        int64  `json:"id"`
	GroupID   int64  `json:"group_id"`
	GroupName string `json:"group_name,omitempty"`
	DayOfWeek string `json:"day_of_week"` // SEMUA, SENIN, SELASA, RABU, KAMIS, JUMAT, SABTU, MINGGU
	Time      string `json:"time"`        // HH:MM
	Title     string `json:"title"`
	AudioCode string `json:"audio_code"`
	AudioName string `json:"audio_name,omitempty"`
	// NextAudioCode: optional follow-up audio(s) played automatically, in order,
	// right after the main bell audio finishes. Comma separated codes,
	// e.g. "AUD-16,FLD-01" = Indonesia Raya, then a random Lagu Daerah.
	// NextAudioName is the display text, e.g. "Indonesia Raya → Lagu Daerah ...".
	NextAudioCode string `json:"next_audio_code"`
	NextAudioName string `json:"next_audio_name,omitempty"`
	Notes         string `json:"notes"`
	IsEnabled     bool   `json:"is_enabled"`
	CreatedAt     string `json:"created_at"`
}

type BellLog struct {
	ID          int64  `json:"id"`
	TriggeredAt string `json:"triggered_at"`
	ScheduleID  *int64 `json:"schedule_id"`
	Title       string `json:"title"`
	GroupName   string `json:"group_name"`
	AudioCode   string `json:"audio_code"`
	TriggerType string `json:"trigger_type"` // AUTO, MANUAL, EMERGENCY
	Status      string `json:"status"`       // SUCCESS, SKIPPED_HOLIDAY, ERROR
}

// InitDB initializes SQLite database and runs migrations
func InitDB(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("gagal membuat direktori database: %w", err)
	}

	sqlDB, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("gagal membuka database: %w", err)
	}

	sqlDB.SetMaxOpenConns(1) // SQLite single writer safety

	d := &DB{
		db:   sqlDB,
		path: dbPath,
	}

	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("gagal migrasi database: %w", err)
	}

	if err := d.seedDefaults(); err != nil {
		return nil, fmt.Errorf("gagal inisialisasi data bawaan: %w", err)
	}

	return d, nil
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS groups (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		description TEXT DEFAULT '',
		is_default INTEGER DEFAULT 0,
		is_active INTEGER DEFAULT 0,
		date_range_enabled INTEGER DEFAULT 0,
		start_date TEXT DEFAULT '',
		end_date TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS audio_files (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		code TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		filename TEXT NOT NULL,
		duration_sec INTEGER DEFAULT 0,
		description TEXT DEFAULT '',
		is_builtin INTEGER DEFAULT 0,
		is_folder INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS schedules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		group_id INTEGER NOT NULL,
		day_of_week TEXT NOT NULL,
		time TEXT NOT NULL,
		title TEXT NOT NULL,
		audio_code TEXT NOT NULL,
		notes TEXT DEFAULT '',
		is_enabled INTEGER DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE CASCADE,
		FOREIGN KEY (audio_code) REFERENCES audio_files(code) ON UPDATE CASCADE
	);

	CREATE TABLE IF NOT EXISTS bell_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		triggered_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		schedule_id INTEGER,
		title TEXT NOT NULL,
		group_name TEXT DEFAULT '',
		audio_code TEXT DEFAULT '',
		trigger_type TEXT NOT NULL,
		status TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		expires_at DATETIME NOT NULL
	);
	`
	if _, err := d.db.Exec(schema); err != nil {
		return err
	}
	// Migrate column is_folder if not exists
	_, _ = d.db.Exec("ALTER TABLE audio_files ADD COLUMN is_folder INTEGER DEFAULT 0")
	// Migrate column next_audio_code (follow-up audio after the bell) if not exists
	_, _ = d.db.Exec("ALTER TABLE schedules ADD COLUMN next_audio_code TEXT DEFAULT ''")
	return nil
}

func (d *DB) seedDefaults() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 1. Seed Settings
	defaultSettings := map[string]string{
		"admin_pin":            "1234",
		"school_name":          "SMK / SMA BUDUT",
		"holiday_mode":         "false",
		"server_audio_enabled": "true",
		"web_audio_default":    "true",
		"server_volume":        "100",
	}

	for k, v := range defaultSettings {
		_, _ = d.db.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)`, k, v)
	}

	// 2. Seed Default Audio Files
	defaultAudios := []struct {
		code, name, filename, desc string
		dur                        int
	}{
		{"AUD-01", "Bel 3 Nada (Do-Mi-Sol)", "bel_3_nada.wav", "Nada merdu 3 chord, cocok untuk bel istirahat/masuk santai", 3},
		{"AUD-02", "Bel 2 Nada (Ding-Dong)", "bel_2_nada.wav", "Nada ganda singkat, cocok untuk panggilan/pergantian jam", 2},
		{"AUD-03", "Bel 4 Nada (Westminster)", "bel_4_nada.wav", "Nada klasik khas jam sekolah/menara, cocok bel masuk & pulang", 4},
		{"AUD-04", "Sirine Darurat", "sirine_darurat.wav", "Nada sirine gelombang tinggi untuk situasi darurat/evakuasi", 6},
		{"AUD-05", "Bel Tit Pendek", "bel_tit_pendek.wav", "Bunyi bip tunggal singkat dan tegas untuk tanda jam pas", 1},
	}

	for _, a := range defaultAudios {
		_, _ = d.db.Exec(`
			INSERT OR IGNORE INTO audio_files (code, name, filename, duration_sec, description, is_builtin)
			VALUES (?, ?, ?, ?, ?, 1)
		`, a.code, a.name, a.filename, a.dur, a.desc)
	}

	// 3. Seed Default Groups
	var count int
	_ = d.db.QueryRow(`SELECT COUNT(*) FROM groups`).Scan(&count)
	if count == 0 {
		res, err := d.db.Exec(`
			INSERT INTO groups (name, description, is_default, is_active, date_range_enabled)
			VALUES (?, ?, 1, 1, 0)
		`, "Grup Reguler", "Jadwal kegiatan belajar mengajar reguler harian (Senin - Jumat)")
		if err == nil {
			regulerID, _ := res.LastInsertId()
			// Insert Sample Schedules for Reguler
			schedules := []struct {
				day, timeStr, title, audioCode, notes string
			}{
				// Senin - Kamis
				{"SENIN", "06:45", "Persiapan Upacara / Masuk", "AUD-02", "Siswa berbaris di lapangan"},
				{"SENIN", "07:00", "Bel Masuk & Upacara Bendera", "AUD-03", "Upacara bendera dimulai"},
				{"SENIN", "09:45", "Istirahat Pertama", "AUD-01", "Istirahat 30 menit"},
				{"SENIN", "10:15", "Masuk Kelas Setelah Istirahat", "AUD-02", "Melanjutkan KBM"},
				{"SENIN", "12:00", "Istirahat Kedua & Shalat Dzuhur", "AUD-01", "Istirahat 45 menit"},
				{"SENIN", "12:45", "Masuk Kelas Siang", "AUD-02", "KBM jam ke-7"},
				{"SENIN", "15:30", "Bel Pulang Sekolah", "AUD-03", "Doa penutup dan kepulangan"},

				{"SELASA", "07:00", "Bel Masuk Kelas", "AUD-03", "KBM jam pertama"},
				{"SELASA", "09:45", "Istirahat Pertama", "AUD-01", "Istirahat 30 menit"},
				{"SELASA", "10:15", "Masuk Kelas", "AUD-02", "Melanjutkan KBM"},
				{"SELASA", "12:00", "Istirahat Kedua & Dzuhur", "AUD-01", "Istirahat 45 menit"},
				{"SELASA", "12:45", "Masuk Kelas Siang", "AUD-02", "KBM jam ke-7"},
				{"SELASA", "15:30", "Bel Pulang Sekolah", "AUD-03", "KBM selesai"},

				{"RABU", "07:00", "Bel Masuk Kelas", "AUD-03", "KBM jam pertama"},
				{"RABU", "09:45", "Istirahat Pertama", "AUD-01", "Istirahat 30 menit"},
				{"RABU", "10:15", "Masuk Kelas", "AUD-02", "Melanjutkan KBM"},
				{"RABU", "12:00", "Istirahat Kedua & Dzuhur", "AUD-01", "Istirahat 45 menit"},
				{"RABU", "12:45", "Masuk Kelas Siang", "AUD-02", "KBM jam ke-7"},
				{"RABU", "15:30", "Bel Pulang Sekolah", "AUD-03", "KBM selesai"},

				{"KAMIS", "07:00", "Bel Masuk Kelas", "AUD-03", "KBM jam pertama"},
				{"KAMIS", "09:45", "Istirahat Pertama", "AUD-01", "Istirahat 30 menit"},
				{"KAMIS", "10:15", "Masuk Kelas", "AUD-02", "Melanjutkan KBM"},
				{"KAMIS", "12:00", "Istirahat Kedua & Dzuhur", "AUD-01", "Istirahat 45 menit"},
				{"KAMIS", "12:45", "Masuk Kelas Siang", "AUD-02", "KBM jam ke-7"},
				{"KAMIS", "15:30", "Bel Pulang Sekolah", "AUD-03", "KBM selesai"},

				// Jumat (Jam Pendek)
				{"JUMAT", "07:00", "Bel Masuk Kelas (Jumat Berkah)", "AUD-03", "KBM jam pertama / literasi / mengaji"},
				{"JUMAT", "09:30", "Istirahat Jumat", "AUD-01", "Istirahat 30 menit"},
				{"JUMAT", "10:00", "Masuk Kelas", "AUD-02", "KBM jam ke-4"},
				{"JUMAT", "11:30", "Bel Pulang & Persiapan Shalat Jumat", "AUD-03", "Kegiatan KBM Jumat berakhir"},
			}

			for _, s := range schedules {
				_, _ = d.db.Exec(`
					INSERT INTO schedules (group_id, day_of_week, time, title, audio_code, notes, is_enabled)
					VALUES (?, ?, ?, ?, ?, ?, 1)
				`, regulerID, s.day, s.timeStr, s.title, s.audioCode, s.notes)
			}
		}

		// Insert Grup Ujian template
		resUjian, err := d.db.Exec(`
			INSERT INTO groups (name, description, is_default, is_active, date_range_enabled, start_date, end_date)
			VALUES (?, ?, 0, 0, 0, '', '')
		`, "Grup Ujian (PTS / PAS)", "Jadwal khusus saat pekan penilaian tengah/akhir semester")
		if err == nil {
			ujianID, _ := resUjian.LastInsertId()
			ujianSchedules := []struct {
				day, timeStr, title, audioCode, notes string
			}{
				{"SEMUA", "07:15", "Masuk Ruang Ujian & Pengisian Hadir", "AUD-02", "Pengawas & peserta masuk ruangan"},
				{"SEMUA", "07:30", "Mulai Ujian Sesi 1", "AUD-03", "Pengerjaan soal sesi 1 dimulai"},
				{"SEMUA", "09:30", "Selesai Ujian Sesi 1 & Istirahat", "AUD-01", "Lembar jawaban dikumpulkan"},
				{"SEMUA", "10:00", "Masuk Ruang Ujian Sesi 2", "AUD-02", "Pengerjaan soal sesi 2"},
				{"SEMUA", "10:15", "Mulai Ujian Sesi 2", "AUD-03", "Pengerjaan soal sesi 2 dimulai"},
				{"SEMUA", "12:15", "Ujian Selesai / Pulang", "AUD-03", "Ujian hari ini selesai"},
			}
			for _, us := range ujianSchedules {
				_, _ = d.db.Exec(`
					INSERT INTO schedules (group_id, day_of_week, time, title, audio_code, notes, is_enabled)
					VALUES (?, ?, ?, ?, ?, ?, 1)
				`, ujianID, us.day, us.timeStr, us.title, us.audioCode, us.notes)
			}
		}

		// Insert Grup Ramadhan template
		_, _ = d.db.Exec(`
			INSERT INTO groups (name, description, is_default, is_active, date_range_enabled)
			VALUES (?, ?, 0, 0, 0)
		`, "Grup Bulan Ramadhan", "Jadwal jam masuk dan pulang yang dipadatkan selama bulan puasa")
	}

	return nil
}

// ----------------- GROUPS CRUD -----------------

func (d *DB) GetGroups() ([]Group, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT g.id, g.name, g.description, g.is_default, g.is_active, 
		       g.date_range_enabled, g.start_date, g.end_date, g.created_at,
		       COUNT(s.id) as schedule_count
		FROM groups g
		LEFT JOIN schedules s ON s.group_id = g.id
		GROUP BY g.id
		ORDER BY g.is_default DESC, g.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		var isDef, isAct, dateEn int
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &isDef, &isAct, &dateEn, &g.StartDate, &g.EndDate, &g.CreatedAt, &g.ScheduleCount); err != nil {
			return nil, err
		}
		g.IsDefault = isDef == 1
		g.IsActive = isAct == 1
		g.DateRangeEnabled = dateEn == 1
		groups = append(groups, g)
	}
	return groups, nil
}

func (d *DB) GetGroupByID(id int64) (*Group, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var g Group
	var isDef, isAct, dateEn int
	err := d.db.QueryRow(`
		SELECT id, name, description, is_default, is_active, date_range_enabled, start_date, end_date, created_at
		FROM groups WHERE id = ?
	`, id).Scan(&g.ID, &g.Name, &g.Description, &isDef, &isAct, &dateEn, &g.StartDate, &g.EndDate, &g.CreatedAt)
	if err != nil {
		return nil, err
	}
	g.IsDefault = isDef == 1
	g.IsActive = isAct == 1
	g.DateRangeEnabled = dateEn == 1
	return &g, nil
}

func (d *DB) GetGroupByName(name string) (*Group, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var g Group
	var isDef, isAct, dateEn int
	err := d.db.QueryRow(`
		SELECT id, name, description, is_default, is_active, date_range_enabled, start_date, end_date, created_at
		FROM groups WHERE LOWER(name) = LOWER(?)
	`, strings.TrimSpace(name)).Scan(&g.ID, &g.Name, &g.Description, &isDef, &isAct, &dateEn, &g.StartDate, &g.EndDate, &g.CreatedAt)
	if err != nil {
		return nil, err
	}
	g.IsDefault = isDef == 1
	g.IsActive = isAct == 1
	g.DateRangeEnabled = dateEn == 1
	return &g, nil
}

func (d *DB) CreateGroup(name, description string) (*Group, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	res, err := d.db.Exec(`INSERT INTO groups (name, description) VALUES (?, ?)`, strings.TrimSpace(name), description)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Group{ID: id, Name: name, Description: description}, nil
}

func (d *DB) UpdateGroup(id int64, name, description string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE groups SET name = ?, description = ? WHERE id = ?`, strings.TrimSpace(name), description, id)
	return err
}

func (d *DB) DeleteGroup(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Do not allow deleting default group
	var isDefault int
	_ = d.db.QueryRow(`SELECT is_default FROM groups WHERE id = ?`, id).Scan(&isDefault)
	if isDefault == 1 {
		return fmt.Errorf("grup default tidak boleh dihapus")
	}

	_, err := d.db.Exec(`DELETE FROM groups WHERE id = ?`, id)
	return err
}

func (d *DB) SetActiveGroup(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE groups SET is_active = 0`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE groups SET is_active = 1 WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) SetGroupDateRange(id int64, enabled bool, startDate, endDate string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	enVal := 0
	if enabled {
		enVal = 1
	}
	_, err := d.db.Exec(`
		UPDATE groups 
		SET date_range_enabled = ?, start_date = ?, end_date = ? 
		WHERE id = ?
	`, enVal, strings.TrimSpace(startDate), strings.TrimSpace(endDate), id)
	return err
}

// ----------------- AUDIO FILES CRUD -----------------

func (d *DB) GetAudioFiles() ([]AudioFile, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, code, name, filename, duration_sec, description, is_builtin, is_folder, created_at
		FROM audio_files
		ORDER BY is_folder DESC, code ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []AudioFile
	for rows.Next() {
		var a AudioFile
		var isBuilt, isFld int
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.Filename, &a.DurationSec, &a.Description, &isBuilt, &isFld, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.IsBuiltin = isBuilt == 1
		a.IsFolder = isFld == 1
		list = append(list, a)
	}
	return list, nil
}

func (d *DB) GetAudioByCode(code string) (*AudioFile, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var a AudioFile
	var isBuilt, isFld int
	err := d.db.QueryRow(`
		SELECT id, code, name, filename, duration_sec, description, is_builtin, is_folder, created_at
		FROM audio_files WHERE LOWER(code) = LOWER(?)
	`, strings.TrimSpace(code)).Scan(&a.ID, &a.Code, &a.Name, &a.Filename, &a.DurationSec, &a.Description, &isBuilt, &isFld, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	a.IsBuiltin = isBuilt == 1
	a.IsFolder = isFld == 1
	return &a, nil
}

func (d *DB) GenerateNextAudioCode() (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT code FROM audio_files WHERE code LIKE 'AUD-%'`)
	if err != nil {
		return "AUD-01", err
	}
	defer rows.Close()

	maxNum := 0
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			var n int
			if _, err := fmt.Sscanf(c, "AUD-%d", &n); err == nil && n > maxNum {
				maxNum = n
			}
		}
	}
	return fmt.Sprintf("AUD-%02d", maxNum+1), nil
}

func (d *DB) AddAudioFile(name, filename, description string, durationSec int, isFolder bool) (*AudioFile, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Generate next code
	var nextCode string
	if isFolder {
		rows, err := d.db.Query(`SELECT code FROM audio_files WHERE code LIKE 'FLD-%'`)
		maxFldNum := 0
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var c string
				if rows.Scan(&c) == nil {
					var n int
					if _, err := fmt.Sscanf(c, "FLD-%d", &n); err == nil && n > maxFldNum {
						maxFldNum = n
					}
				}
			}
		}
		nextCode = fmt.Sprintf("FLD-%02d", maxFldNum+1)
	} else {
		rows, err := d.db.Query(`SELECT code FROM audio_files WHERE code LIKE 'AUD-%'`)
		maxAudNum := 0
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var c string
				if rows.Scan(&c) == nil {
					var n int
					if _, err := fmt.Sscanf(c, "AUD-%d", &n); err == nil && n > maxAudNum {
						maxAudNum = n
					}
				}
			}
		}
		nextCode = fmt.Sprintf("AUD-%02d", maxAudNum+1)
	}

	fldVal := 0
	if isFolder {
		fldVal = 1
	}

	res, err := d.db.Exec(`
		INSERT INTO audio_files (code, name, filename, duration_sec, description, is_builtin, is_folder)
		VALUES (?, ?, ?, ?, ?, 0, ?)
	`, nextCode, strings.TrimSpace(name), filename, durationSec, strings.TrimSpace(description), fldVal)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &AudioFile{
		ID:          id,
		Code:        nextCode,
		Name:        name,
		Filename:    filename,
		DurationSec: durationSec,
		Description: description,
		IsBuiltin:   false,
		IsFolder:    isFolder,
	}, nil
}

func (d *DB) DeleteAudioFile(id int64) (*AudioFile, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var a AudioFile
	var isBuilt int
	err := d.db.QueryRow(`SELECT id, code, filename, is_builtin FROM audio_files WHERE id = ?`, id).Scan(&a.ID, &a.Code, &a.Filename, &isBuilt)
	if err != nil {
		return nil, err
	}
	if isBuilt == 1 {
		return nil, fmt.Errorf("audio bawaan sistem tidak boleh dihapus")
	}

	// Check if used in any schedule
	var usedCount int
	_ = d.db.QueryRow(`SELECT COUNT(*) FROM schedules WHERE audio_code = ? OR (',' || REPLACE(COALESCE(next_audio_code, ''), ' ', '') || ',') LIKE ?`, a.Code, "%,"+a.Code+",%").Scan(&usedCount)
	if usedCount > 0 {
		return nil, fmt.Errorf("audio sedang digunakan oleh %d jadwal, tidak bisa dihapus", usedCount)
	}

	_, err = d.db.Exec(`DELETE FROM audio_files WHERE id = ?`, id)
	return &a, err
}

// ----------------- SCHEDULES CRUD -----------------

func (d *DB) GetSchedules(groupID int64, dayOfWeek string) ([]Schedule, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT s.id, s.group_id, g.name as group_name, s.day_of_week, s.time, s.title, 
		       s.audio_code, COALESCE(a.name, '') as audio_name,
		       COALESCE(s.next_audio_code, ''),
		       s.notes, s.is_enabled, s.created_at
		FROM schedules s
		JOIN groups g ON g.id = s.group_id
		LEFT JOIN audio_files a ON a.code = s.audio_code
		WHERE 1=1
	`
	var args []interface{}

	if groupID > 0 {
		query += " AND s.group_id = ?"
		args = append(args, groupID)
	}

	if dayOfWeek != "" && dayOfWeek != "ALL" {
		targetDay := strings.ToUpper(strings.TrimSpace(dayOfWeek))
		switch targetDay {
		case "SENIN":
			query += " AND (s.day_of_week = 'SENIN' OR s.day_of_week = 'SEMUA' OR s.day_of_week = 'SENIN - JUMAT' OR s.day_of_week = 'SENIN - KAMIS' OR s.day_of_week = 'SENIN - RABU' OR s.day_of_week LIKE '%SENIN%')"
		case "SELASA", "RABU":
			query += " AND (s.day_of_week = ? OR s.day_of_week = 'SEMUA' OR s.day_of_week = 'SENIN - JUMAT' OR s.day_of_week = 'SENIN - KAMIS' OR s.day_of_week = 'SELASA - JUMAT' OR s.day_of_week = 'SENIN - RABU' OR s.day_of_week LIKE ?)"
			args = append(args, targetDay, "%"+targetDay+"%")
		case "KAMIS":
			query += " AND (s.day_of_week = 'KAMIS' OR s.day_of_week = 'SEMUA' OR s.day_of_week = 'SENIN - JUMAT' OR s.day_of_week = 'SENIN - KAMIS' OR s.day_of_week = 'SELASA - JUMAT' OR s.day_of_week LIKE '%KAMIS%')"
		case "JUMAT":
			query += " AND (s.day_of_week = 'JUMAT' OR s.day_of_week = 'SEMUA' OR s.day_of_week = 'SENIN - JUMAT' OR s.day_of_week = 'SELASA - JUMAT' OR s.day_of_week LIKE '%JUMAT%')"
		default:
			query += " AND (s.day_of_week = ? OR s.day_of_week = 'SEMUA' OR s.day_of_week LIKE ?)"
			args = append(args, targetDay, "%"+targetDay+"%")
		}
	}

	query += ` ORDER BY s.time ASC, s.id ASC `

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Schedule
	for rows.Next() {
		var s Schedule
		var isEn int
		if err := rows.Scan(&s.ID, &s.GroupID, &s.GroupName, &s.DayOfWeek, &s.Time, &s.Title,
			&s.AudioCode, &s.AudioName, &s.NextAudioCode, &s.Notes, &isEn, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.IsEnabled = isEn == 1
		list = append(list, s)
	}
	rows.Close()

	// Resolve display names of follow-up audio chains
	needNames := false
	for _, s := range list {
		if s.NextAudioCode != "" {
			needNames = true
			break
		}
	}
	if needNames {
		names := make(map[string]string)
		if nrows, err := d.db.Query(`SELECT code, name FROM audio_files`); err == nil {
			for nrows.Next() {
				var c, n string
				if nrows.Scan(&c, &n) == nil {
					names[strings.ToUpper(c)] = n
				}
			}
			nrows.Close()
		}
		for i := range list {
			var parts []string
			for _, c := range SplitAudioCodes(list[i].NextAudioCode) {
				if n, ok := names[c]; ok {
					parts = append(parts, n)
				} else {
					parts = append(parts, c)
				}
			}
			list[i].NextAudioName = strings.Join(parts, " → ")
		}
	}
	return list, nil
}

// SplitAudioCodes parses a comma separated follow-up chain ("AUD-16, FLD-01") into clean upper-case codes.
func SplitAudioCodes(v string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' || r == '|' }) {
		if c := strings.ToUpper(strings.TrimSpace(p)); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// JoinAudioCodes is the storage form of a follow-up chain.
func JoinAudioCodes(codes []string) string {
	return strings.Join(codes, ",")
}

func (d *DB) CreateSchedule(s *Schedule) (*Schedule, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	enVal := 1
	if !s.IsEnabled {
		enVal = 0
	}

	res, err := d.db.Exec(`
		INSERT INTO schedules (group_id, day_of_week, time, title, audio_code, next_audio_code, notes, is_enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, s.GroupID, strings.ToUpper(s.DayOfWeek), s.Time, s.Title, s.AudioCode, JoinAudioCodes(SplitAudioCodes(s.NextAudioCode)), s.Notes, enVal)
	if err != nil {
		return nil, err
	}
	s.ID, _ = res.LastInsertId()
	return s, nil
}

func (d *DB) UpdateSchedule(s *Schedule) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	enVal := 1
	if !s.IsEnabled {
		enVal = 0
	}

	_, err := d.db.Exec(`
		UPDATE schedules 
		SET group_id = ?, day_of_week = ?, time = ?, title = ?, audio_code = ?, next_audio_code = ?, notes = ?, is_enabled = ?
		WHERE id = ?
	`, s.GroupID, strings.ToUpper(s.DayOfWeek), s.Time, s.Title, s.AudioCode, JoinAudioCodes(SplitAudioCodes(s.NextAudioCode)), s.Notes, enVal, s.ID)
	return err
}

func (d *DB) ToggleSchedule(id int64) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var cur int
	err := d.db.QueryRow(`SELECT is_enabled FROM schedules WHERE id = ?`, id).Scan(&cur)
	if err != nil {
		return false, err
	}
	newVal := 1
	if cur == 1 {
		newVal = 0
	}
	_, err = d.db.Exec(`UPDATE schedules SET is_enabled = ? WHERE id = ?`, newVal, id)
	return newVal == 1, err
}

func (d *DB) DeleteSchedule(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM schedules WHERE id = ?`, id)
	return err
}

// ----------------- SETTINGS & LOGS -----------------

func (d *DB) GetSetting(key, defaultVal string) string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var val string
	err := d.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&val)
	if err != nil {
		return defaultVal
	}
	return val
}

func (d *DB) SetSetting(key, val string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, val)
	return err
}

func (d *DB) GetAllSettings() (map[string]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			settings[k] = v
		}
	}
	return settings, nil
}

func (d *DB) LogBell(scheduleID *int64, title, groupName, audioCode, triggerType, status string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO bell_logs (schedule_id, title, group_name, audio_code, trigger_type, status)
		VALUES (?, ?, ?, ?, ?, ?)
	`, scheduleID, title, groupName, audioCode, triggerType, status)
	return err
}

func (d *DB) GetRecentLogs(limit int) ([]BellLog, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.Query(`
		SELECT id, triggered_at, schedule_id, title, group_name, audio_code, trigger_type, status
		FROM bell_logs
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []BellLog
	for rows.Next() {
		var l BellLog
		if err := rows.Scan(&l.ID, &l.TriggeredAt, &l.ScheduleID, &l.Title, &l.GroupName, &l.AudioCode, &l.TriggerType, &l.Status); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

// ----------------- BACKUP & RESTORE -----------------

func (d *DB) BackupDatabase(w io.Writer) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	file, err := os.Open(d.path)
	if err != nil {
		return fmt.Errorf("gagal membuka file database: %w", err)
	}
	defer file.Close()

	_, err = io.Copy(w, file)
	return err
}

func (d *DB) RestoreDatabase(r io.Reader) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Close current DB handle
	if err := d.db.Close(); err != nil {
		return fmt.Errorf("gagal menutup database sebelum restore: %w", err)
	}

	backupPath := d.path + ".bak." + time.Now().Format("20060102150405")
	_ = os.Rename(d.path, backupPath)

	out, err := os.Create(d.path)
	if err != nil {
		// Try reverting
		_ = os.Rename(backupPath, d.path)
		return fmt.Errorf("gagal membuat file baru untuk restore: %w", err)
	}

	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		_ = os.Rename(backupPath, d.path)
		return fmt.Errorf("gagal menulis data restore: %w", err)
	}
	out.Close()

	// Reopen database
	newDB, err := sql.Open("sqlite", d.path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return fmt.Errorf("gagal membuka database hasil restore: %w", err)
	}
	newDB.SetMaxOpenConns(1)
	d.db = newDB

	return nil
}

// ----------------- PERSISTENT SESSIONS -----------------

func (d *DB) CreateSession(token string, expiresAt time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(`INSERT OR REPLACE INTO sessions (token, expires_at) VALUES (?, ?)`, token, expiresAt)
	return err
}

func (d *DB) IsValidSession(token string) bool {
	if token == "" {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	var exp time.Time
	err := d.db.QueryRow(`SELECT expires_at FROM sessions WHERE token = ?`, token).Scan(&exp)
	if err != nil {
		return false
	}
	return time.Now().Before(exp)
}

func (d *DB) DeleteSession(token string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}
