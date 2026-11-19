package audio

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Player interface {
	Play(filePath string) error
	Stop() error
	IsPlaying() bool
	CurrentFile() string
	SetOnEnd(fn func())
}

// QueueItem is an audio that will be played automatically after the current one ends.
type QueueItem struct {
	Path string // file or folder path (folders are resolved randomly when played)
	Code string
	Name string
}

type Manager struct {
	playMu      sync.Mutex // serializes every start/stop so a chained play never overrides a newer action
	gen         uint64     // incremented on every explicit PlayFile/Stop
	pending     []QueueItem
	onStart     func(code, name string)
	onPlay      func(code, name string) // fired whenever any audio starts on the speaker
	player      Player
	audioDir    string
	mu          sync.RWMutex
	currentCode string
	currentName string
	lastPlayed  map[string]string // folder path -> last played filename
	rng         *rand.Rand
	onEnd       func(code, name string)
}

func NewManager(audioDir string) *Manager {
	return newManagerWithPlayer(audioDir, newPlatformPlayer())
}

func newManagerWithPlayer(audioDir string, player Player) *Manager {
	m := &Manager{
		player:     player,
		audioDir:   audioDir,
		lastPlayed: make(map[string]string),
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	if m.player != nil {
		m.player.SetOnEnd(func() {
			m.mu.Lock()
			code := m.currentCode
			name := m.currentName
			m.currentCode = ""
			m.currentName = ""
			cb := m.onEnd
			gen := m.gen
			m.mu.Unlock()
			if cb != nil {
				cb(code, name)
			}
			m.playNextQueued(gen)
		})
	}
	return m
}

func (m *Manager) SetOnEnd(fn func(code, name string)) {
	m.mu.Lock()
	m.onEnd = fn
	m.mu.Unlock()
}

// ResolvePlayPath checks if path is a file or a folder.
// If it is a folder, randomly chooses one audio file from that folder.
func (m *Manager) ResolvePlayPath(path, defaultName string) (string, string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("file atau folder audio tidak ditemukan: %w", err)
	}

	if !fi.IsDir() {
		return path, defaultName, nil
	}

	// Read audio files in the folder
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", "", fmt.Errorf("gagal membaca isi folder audio: %w", err)
	}

	var audioFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".mp3" || ext == ".wav" || ext == ".ogg" {
			audioFiles = append(audioFiles, e.Name())
		}
	}

	if len(audioFiles) == 0 {
		return "", "", fmt.Errorf("tidak ada file audio (.mp3/.wav/.ogg) di folder '%s'", filepath.Base(path))
	}

	m.mu.Lock()
	last := m.lastPlayed[path]
	var chosen string
	if len(audioFiles) == 1 {
		chosen = audioFiles[0]
	} else {
		// Pick one different from last if possible
		var candidates []string
		for _, f := range audioFiles {
			if f != last {
				candidates = append(candidates, f)
			}
		}
		if len(candidates) == 0 {
			candidates = audioFiles
		}
		idx := m.rng.Intn(len(candidates))
		chosen = candidates[idx]
	}
	m.lastPlayed[path] = chosen
	m.mu.Unlock()

	fullPath := filepath.Join(path, chosen)
	cleanName := strings.TrimSuffix(chosen, filepath.Ext(chosen))
	dispName := fmt.Sprintf("%s (Lagu: %s)", defaultName, cleanName)
	return fullPath, dispName, nil
}

// SetOnStart registers a callback fired when a queued follow-up audio starts automatically.
func (m *Manager) SetOnStart(fn func(code, name string)) {
	m.mu.Lock()
	m.onStart = fn
	m.mu.Unlock()
}

func (m *Manager) PlayFile(absOrRelPath, code, name string) error {
	return m.PlayFileThen(absOrRelPath, code, name)
}

// PlayFileThen plays an audio and queues follow-up audios that start automatically,
// one after another, as soon as the previous one finishes naturally.
// Any later PlayFile/Stop cancels the remaining queue.
func (m *Manager) PlayFileThen(absOrRelPath, code, name string, next ...QueueItem) error {
	m.playMu.Lock()
	defer m.playMu.Unlock()

	m.mu.Lock()
	m.gen++
	m.pending = append([]QueueItem(nil), next...)
	m.mu.Unlock()

	return m.startLocked(absOrRelPath, code, name)
}

// startLocked resolves and plays one item. Caller must hold playMu.
func (m *Manager) startLocked(path, code, name string) error {
	resolvedPath, dispName, err := m.ResolvePlayPath(path, name)
	if err != nil {
		m.mu.Lock()
		m.pending = nil
		m.mu.Unlock()
		return err
	}

	m.mu.Lock()
	m.currentCode = code
	m.currentName = dispName
	m.mu.Unlock()

	if m.player == nil {
		return errors.New("audio player tidak tersedia pada platform ini")
	}
	if err := m.player.Play(resolvedPath); err != nil {
		return err
	}
	m.mu.RLock()
	playCb := m.onPlay
	m.mu.RUnlock()
	if playCb != nil {
		go playCb(code, dispName)
	}
	return nil
}

// SetOnPlay registers a callback fired whenever audio starts playing on the speaker.
func (m *Manager) SetOnPlay(fn func(code, name string)) {
	m.mu.Lock()
	m.onPlay = fn
	m.mu.Unlock()
}

// playNextQueued starts the next queued follow-up audio if nothing newer happened since gen.
func (m *Manager) playNextQueued(gen uint64) {
	m.playMu.Lock()
	defer m.playMu.Unlock()

	m.mu.Lock()
	if m.gen != gen || len(m.pending) == 0 {
		m.mu.Unlock()
		return
	}
	item := m.pending[0]
	m.pending = m.pending[1:]
	startCb := m.onStart
	m.mu.Unlock()

	if err := m.startLocked(item.Path, item.Code, item.Name); err != nil {
		return
	}
	if startCb != nil {
		startCb(item.Code, item.Name)
	}
}

func (m *Manager) Stop() error {
	m.playMu.Lock()
	defer m.playMu.Unlock()

	m.mu.Lock()
	m.gen++
	m.pending = nil
	m.currentCode = ""
	m.currentName = ""
	m.mu.Unlock()

	if m.player == nil {
		return nil
	}
	return m.player.Stop()
}

func (m *Manager) IsPlaying() bool {
	if m.player == nil {
		return false
	}
	return m.player.IsPlaying()
}

func (m *Manager) CurrentStatus() (bool, string, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.player == nil {
		return false, "", ""
	}
	return m.player.IsPlaying(), m.currentCode, m.currentName
}
