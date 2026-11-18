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

type Manager struct {
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
	m := &Manager{
		player:     newPlatformPlayer(),
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
			m.mu.Unlock()
			if cb != nil {
				cb(code, name)
			}
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

func (m *Manager) PlayFile(absOrRelPath, code, name string) error {
	resolvedPath, dispName, err := m.ResolvePlayPath(absOrRelPath, name)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.currentCode = code
	m.currentName = dispName
	m.mu.Unlock()

	if m.player == nil {
		return errors.New("audio player tidak tersedia pada platform ini")
	}
	return m.player.Play(resolvedPath)
}

func (m *Manager) Stop() error {
	m.mu.Lock()
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
