//go:build windows

package audio

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	winmm          = syscall.NewLazyDLL("winmm.dll")
	mciSendStringW = winmm.NewProc("mciSendStringW")
)

type windowsPlayer struct {
	mu           sync.Mutex
	currentFile  string
	isPlaying    bool
	currentAlias string
	playID       uint64
	durationMs   int64
	startTime    time.Time
	onEnd        func()
}

func newPlatformPlayer() Player {
	return &windowsPlayer{}
}

func (p *windowsPlayer) SetOnEnd(fn func()) {
	p.mu.Lock()
	p.onEnd = fn
	p.mu.Unlock()
}

func (p *windowsPlayer) sendMCI(command string) (string, error) {
	cmdPtr, err := syscall.UTF16PtrFromString(command)
	if err != nil {
		return "", err
	}

	buf := make([]uint16, 256)
	r1, _, _ := mciSendStringW.Call(
		uintptr(unsafe.Pointer(cmdPtr)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		0,
	)

	if r1 != 0 {
		return "", fmt.Errorf("MCI error %d executing: %s", r1, command)
	}

	return syscall.UTF16ToString(buf), nil
}

func (p *windowsPlayer) Play(filePath string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.playID++
	currentID := p.playID

	// 1. Close previously active alias immediately
	if p.currentAlias != "" {
		p.sendMCI(fmt.Sprintf("stop %s", p.currentAlias))
		p.sendMCI(fmt.Sprintf("close %s", p.currentAlias))
		p.currentAlias = ""
	}
	p.sendMCI("stop all")
	p.sendMCI("close all")
	p.isPlaying = false
	p.currentFile = ""

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	// 2. Generate unique alias per playID to avoid device locked / name conflicts
	alias := fmt.Sprintf("bell_dev_%d", currentID)

	openCmd := fmt.Sprintf(`open "%s" alias %s`, absPath, alias)
	if _, err := p.sendMCI(openCmd); err != nil {
		ext := strings.ToLower(filepath.Ext(absPath))
		if ext == ".mp3" {
			openCmd = fmt.Sprintf(`open "%s" type mpegvideo alias %s`, absPath, alias)
		} else if ext == ".wav" {
			openCmd = fmt.Sprintf(`open "%s" type waveaudio alias %s`, absPath, alias)
		}
		if _, err2 := p.sendMCI(openCmd); err2 != nil {
			return fmt.Errorf("gagal membuka audio: %w", err2)
		}
	}

	// 3. Query duration in milliseconds
	p.sendMCI(fmt.Sprintf("set %s time format milliseconds", alias))
	lenStr, _ := p.sendMCI(fmt.Sprintf("status %s length", alias))
	durMs, _ := strconv.ParseInt(strings.TrimSpace(lenStr), 10, 64)
	p.durationMs = durMs
	p.startTime = time.Now()

	playCmd := fmt.Sprintf("play %s", alias)
	if _, err := p.sendMCI(playCmd); err != nil {
		p.sendMCI(fmt.Sprintf("close %s", alias))
		return fmt.Errorf("gagal memutar audio: %w", err)
	}

	p.currentAlias = alias
	p.currentFile = absPath
	p.isPlaying = true

	// 4. Monitor playback end in a background goroutine with duration awareness
	go func(id uint64, targetAlias string, durationMs int64, startTime time.Time) {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		consecutiveStopped := 0

		for range ticker.C {
			p.mu.Lock()
			if p.playID != id || !p.isPlaying {
				p.mu.Unlock()
				return
			}

			elapsedMs := time.Since(startTime).Milliseconds()

			// Safety 1: Never trigger finish within the first 1500ms of any track
			if elapsedMs < 1500 {
				p.mu.Unlock()
				continue
			}

			// Safety 2: While elapsed time is well within known duration, playback is guaranteed running
			if durationMs > 2000 && elapsedMs < durationMs-500 {
				p.mu.Unlock()
				continue
			}

			status, _ := p.sendMCI(fmt.Sprintf("status %s mode", targetAlias))
			posStr, _ := p.sendMCI(fmt.Sprintf("status %s position", targetAlias))
			posMs, _ := strconv.ParseInt(strings.TrimSpace(posStr), 10, 64)

			isFinished := false
			if durationMs > 0 {
				if (posMs > 0 && posMs >= durationMs-200) || elapsedMs >= durationMs+2000 {
					isFinished = true
				}
			}

			if status == "stopped" {
				consecutiveStopped++
				if consecutiveStopped >= 2 && elapsedMs >= 2000 {
					isFinished = true
				}
			} else if status == "playing" {
				consecutiveStopped = 0
			}

			if isFinished {
				if p.playID == id {
					p.isPlaying = false
					p.currentFile = ""
					p.currentAlias = ""
					p.sendMCI(fmt.Sprintf("stop %s", targetAlias))
					p.sendMCI(fmt.Sprintf("close %s", targetAlias))
					onEndFn := p.onEnd
					p.mu.Unlock()
					if onEndFn != nil {
						go onEndFn()
					}
					return
				}
				p.mu.Unlock()
				return
			}
			p.mu.Unlock()
		}
	}(currentID, alias, durMs, p.startTime)

	return nil
}

func (p *windowsPlayer) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.playID++
	p.isPlaying = false
	p.currentFile = ""
	if p.currentAlias != "" {
		p.sendMCI(fmt.Sprintf("stop %s", p.currentAlias))
		p.sendMCI(fmt.Sprintf("close %s", p.currentAlias))
		p.currentAlias = ""
	}
	p.sendMCI("stop all")
	p.sendMCI("close all")
	return nil
}

func (p *windowsPlayer) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isPlaying
}

func (p *windowsPlayer) CurrentFile() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentFile
}
