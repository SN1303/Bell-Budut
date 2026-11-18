package scheduler

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"bell-budut/audio"
	"bell-budut/database"
)

type NextBellInfo struct {
	Title            string `json:"title"`
	Time             string `json:"time"`
	Day              string `json:"day"`
	AudioCode        string `json:"audio_code"`
	AudioName        string `json:"audio_name"`
	CountdownSec     int    `json:"countdown_sec"`
	CountdownDisplay string `json:"countdown_display"`
	Notes            string `json:"notes"`
}

type ActiveGroupStatus struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	DeterminedBy     string `json:"determined_by"` // DATE_RANGE or MANUAL or DEFAULT
	StartDate        string `json:"start_date,omitempty"`
	EndDate          string `json:"end_date,omitempty"`
	DateRangeEnabled bool   `json:"date_range_enabled"`
}

type StatusPayload struct {
	Type               string             `json:"type"`
	ServerTime         string             `json:"server_time"`         // 07:15:30
	ServerDate         string             `json:"server_date"`         // Rabu, 18 Nov 2026
	DayName            string             `json:"day_name"`            // RABU
	HolidayMode        bool               `json:"holiday_mode"`
	ActiveGroup        *ActiveGroupStatus `json:"active_group"`
	NextBell           *NextBellInfo      `json:"next_bell"`
	ServerAudioEnabled bool               `json:"server_audio_enabled"`
	IsServerPlaying    bool               `json:"is_server_playing"`
	CurrentPlaying     string             `json:"current_playing"`
}

type Scheduler struct {
	db          *database.DB
	audioMgr    *audio.Manager
	audioDir    string
	clientsMu   sync.RWMutex
	clients     map[chan []byte]bool
	stopChan    chan struct{}
	lastTrigger string
}

func NewScheduler(db *database.DB, audioMgr *audio.Manager, audioDir string) *Scheduler {
	return &Scheduler{
		db:       db,
		audioMgr: audioMgr,
		audioDir: audioDir,
		clients:  make(map[chan []byte]bool),
		stopChan: make(chan struct{}),
	}
}

func (s *Scheduler) Start() {
	go s.runLoop()
}

func (s *Scheduler) Stop() {
	close(s.stopChan)
}

// SSE Subscription
func (s *Scheduler) Subscribe() chan []byte {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	ch := make(chan []byte, 16)
	s.clients[ch] = true
	return ch
}

func (s *Scheduler) Unsubscribe(ch chan []byte) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if _, ok := s.clients[ch]; ok {
		delete(s.clients, ch)
		close(ch)
	}
}

func (s *Scheduler) Broadcast(data []byte) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	for ch := range s.clients {
		select {
		case ch <- data:
		default:
			// client slow/stalled
		}
	}
}

func (s *Scheduler) runLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case now := <-ticker.C:
			s.handleTick(now)
		}
	}
}

func (s *Scheduler) handleTick(now time.Time) {
	currentDayIndo := getDayIndo(now.Weekday())
	timeHM := now.Format("15:04")
	timeHMS := now.Format("15:04:05")
	dateYMD := now.Format("2006-01-02")
	dateDisplay := formatIndoDate(now)

	activeGroup, _ := s.GetActiveGroup(dateYMD)
	holidayMode := s.db.GetSetting("holiday_mode", "false") == "true"
	serverAudioEnabled := s.db.GetSetting("server_audio_enabled", "true") == "true"
	isPlaying, curCode, curName := s.audioMgr.CurrentStatus()

	var curPlayingStr string
	if isPlaying {
		curPlayingStr = fmt.Sprintf("%s (%s)", curName, curCode)
	}

	// 1. Check if we need to ring bell at the exact minute (second == 0)
	triggerKey := fmt.Sprintf("%s-%s", dateYMD, timeHM)
	if now.Second() == 0 && s.lastTrigger != triggerKey {
		s.lastTrigger = triggerKey
		go s.checkAndTriggerBell(activeGroup, currentDayIndo, timeHM, holidayMode, serverAudioEnabled)
	}

	// 2. Compute Next Bell
	nextBell := s.calculateNextBell(activeGroup, now, currentDayIndo)

	// 3. Broadcast Heartbeat Status to all SSE clients
	status := StatusPayload{
		Type:               "TICK",
		ServerTime:         timeHMS,
		ServerDate:         dateDisplay,
		DayName:            currentDayIndo,
		HolidayMode:        holidayMode,
		ActiveGroup:        activeGroup,
		NextBell:           nextBell,
		ServerAudioEnabled: serverAudioEnabled,
		IsServerPlaying:    isPlaying,
		CurrentPlaying:     curPlayingStr,
	}

	data, err := json.Marshal(status)
	if err == nil {
		s.Broadcast(data)
	}
}

func (s *Scheduler) checkAndTriggerBell(group *ActiveGroupStatus, dayIndo, timeHM string, holidayMode, serverAudioEnabled bool) {
	if group == nil {
		return
	}

	schedules, err := s.db.GetSchedules(group.ID, dayIndo)
	if err != nil {
		return
	}

	for _, sched := range schedules {
		if !sched.IsEnabled || sched.Time != timeHM {
			continue
		}

		// Found matching schedule!
		if holidayMode {
			_ = s.db.LogBell(&sched.ID, sched.Title, group.Name, sched.AudioCode, "AUTO", "SKIPPED_HOLIDAY")
			continue
		}

		// Retrieve audio file info
		audioFile, err := s.db.GetAudioByCode(sched.AudioCode)
		if err != nil {
			_ = s.db.LogBell(&sched.ID, sched.Title, group.Name, sched.AudioCode, "AUTO", "ERROR_AUDIO_NOT_FOUND")
			continue
		}

		// 1. Play on Server Soundcard (if enabled)
		if serverAudioEnabled {
			audioPath := filepath.Join(s.audioDir, audioFile.Filename)
			go func(path, code, name string) {
				_ = s.audioMgr.PlayFile(path, code, name)
			}(audioPath, audioFile.Code, audioFile.Name)
		}

		// 2. Broadcast BELL_RING event to Web Clients (SSE)
		ringPayload := map[string]interface{}{
			"type":        "BELL_RING",
			"schedule_id": sched.ID,
			"title":       sched.Title,
			"group_name":  group.Name,
			"audio_code":  audioFile.Code,
			"audio_name":  audioFile.Name,
			"audio_url":   "/api/audio/stream/" + audioFile.Code,
			"time":        timeHM,
			"notes":       sched.Notes,
		}
		data, _ := json.Marshal(ringPayload)
		s.Broadcast(data)

		// 3. Log to DB
		_ = s.db.LogBell(&sched.ID, sched.Title, group.Name, sched.AudioCode, "AUTO", "SUCCESS")
	}
}

func (s *Scheduler) TriggerManual(audioCode, triggerType, customTitle string) error {
	audioFile, err := s.db.GetAudioByCode(audioCode)
	if err != nil {
		return fmt.Errorf("audio dengan kode %s tidak ditemukan", audioCode)
	}

	title := customTitle
	if title == "" {
		title = "Bel Manual: " + audioFile.Name
	}

	// 1. Play on server
	audioPath := filepath.Join(s.audioDir, audioFile.Filename)
	go func() {
		_ = s.audioMgr.PlayFile(audioPath, audioFile.Code, audioFile.Name)
	}()

	// 2. Broadcast to web
	ringPayload := map[string]interface{}{
		"type":       "BELL_RING",
		"title":      title,
		"group_name": "MANUAL",
		"audio_code": audioFile.Code,
		"audio_name": audioFile.Name,
		"audio_url":  "/api/audio/stream/" + audioFile.Code,
		"time":       time.Now().Format("15:04"),
		"notes":      "Dibunyikan secara manual",
	}
	data, _ := json.Marshal(ringPayload)
	s.Broadcast(data)

	// 3. Log
	_ = s.db.LogBell(nil, title, "MANUAL", audioFile.Code, triggerType, "SUCCESS")
	return nil
}

func (s *Scheduler) GetActiveGroup(todayYMD string) (*ActiveGroupStatus, error) {
	groups, err := s.db.GetGroups()
	if err != nil || len(groups) == 0 {
		return nil, err
	}

	// Priority 1: Date Range Match
	for _, g := range groups {
		if g.DateRangeEnabled && g.StartDate != "" && g.EndDate != "" {
			if g.StartDate <= todayYMD && todayYMD <= g.EndDate {
				return &ActiveGroupStatus{
					ID:               g.ID,
					Name:             g.Name,
					DeterminedBy:     "DATE_RANGE",
					StartDate:        g.StartDate,
					EndDate:          g.EndDate,
					DateRangeEnabled: true,
				}, nil
			}
		}
	}

	// Priority 2: Manual Active Toggle
	for _, g := range groups {
		if g.IsActive {
			return &ActiveGroupStatus{
				ID:               g.ID,
				Name:             g.Name,
				DeterminedBy:     "MANUAL",
				StartDate:        g.StartDate,
				EndDate:          g.EndDate,
				DateRangeEnabled: g.DateRangeEnabled,
			}, nil
		}
	}

	// Priority 3: Default Group
	for _, g := range groups {
		if g.IsDefault {
			return &ActiveGroupStatus{
				ID:               g.ID,
				Name:             g.Name,
				DeterminedBy:     "DEFAULT",
				StartDate:        g.StartDate,
				EndDate:          g.EndDate,
				DateRangeEnabled: g.DateRangeEnabled,
			}, nil
		}
	}

	first := groups[0]
	return &ActiveGroupStatus{
		ID:           first.ID,
		Name:         first.Name,
		DeterminedBy: "DEFAULT",
	}, nil
}

func (s *Scheduler) calculateNextBell(group *ActiveGroupStatus, now time.Time, dayIndo string) *NextBellInfo {
	if group == nil {
		return nil
	}

	schedules, err := s.db.GetSchedules(group.ID, dayIndo)
	if err != nil || len(schedules) == 0 {
		return nil
	}

	curHM := now.Format("15:04")
	nowSecToday := now.Hour()*3600 + now.Minute()*60 + now.Second()

	var candidate *database.Schedule
	var candidateDiffSec int = 99999999

	for _, sc := range schedules {
		if !sc.IsEnabled {
			continue
		}
		// Only consider today's upcoming
		if sc.Time > curHM {
			var h, m int
			_, _ = fmt.Sscanf(sc.Time, "%d:%d", &h, &m)
			targetSec := h*3600 + m*60
			diff := targetSec - nowSecToday
			if diff > 0 && diff < candidateDiffSec {
				candidateDiffSec = diff
				cCopy := sc
				candidate = &cCopy
			}
		}
	}

	if candidate != nil {
		h := candidateDiffSec / 3600
		m := (candidateDiffSec % 3600) / 60
		sec := candidateDiffSec % 60
		var cdStr string
		if h > 0 {
			cdStr = fmt.Sprintf("%02d:%02d:%02d", h, m, sec)
		} else {
			cdStr = fmt.Sprintf("%02d:%02d", m, sec)
		}

		return &NextBellInfo{
			Title:            candidate.Title,
			Time:             candidate.Time,
			Day:              candidate.DayOfWeek,
			AudioCode:        candidate.AudioCode,
			AudioName:        candidate.AudioName,
			CountdownSec:     candidateDiffSec,
			CountdownDisplay: cdStr,
			Notes:            candidate.Notes,
		}
	}

	return nil
}

func getDayIndo(w time.Weekday) string {
	switch w {
	case time.Sunday:
		return "MINGGU"
	case time.Monday:
		return "SENIN"
	case time.Tuesday:
		return "SELASA"
	case time.Wednesday:
		return "RABU"
	case time.Thursday:
		return "KAMIS"
	case time.Friday:
		return "JUMAT"
	case time.Saturday:
		return "SABTU"
	default:
		return "SEMUA"
	}
}

func formatIndoDate(t time.Time) string {
	days := map[time.Weekday]string{
		time.Sunday:    "Minggu",
		time.Monday:    "Senin",
		time.Tuesday:   "Selasa",
		time.Wednesday: "Rabu",
		time.Thursday:  "Kamis",
		time.Friday:    "Jumat",
		time.Saturday:  "Sabtu",
	}
	months := map[time.Month]string{
		time.January:   "Januari",
		time.February:  "Februari",
		time.March:     "Maret",
		time.April:     "April",
		time.May:       "Mei",
		time.June:      "Juni",
		time.July:      "Juli",
		time.August:     "Agustus",
		time.September: "September",
		time.October:   "Oktober",
		time.November:  "November",
		time.December:  "Desember",
	}
	return fmt.Sprintf("%s, %d %s %d", days[t.Weekday()], t.Day(), months[t.Month()], t.Year())
}
