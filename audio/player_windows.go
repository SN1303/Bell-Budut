//go:build windows

package audio

import (
	"fmt"
	"path/filepath"
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
	mu          sync.Mutex
	currentFile string
	isPlaying   bool
	alias       string
	playID      uint64
}

func newPlatformPlayer() Player {
	return &windowsPlayer{
		alias: "school_bell_active",
	}
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

	// Stop and close any previously playing audio unconditionally
	p.sendMCI(fmt.Sprintf("stop %s", p.alias))
	p.sendMCI(fmt.Sprintf("close %s", p.alias))
	p.sendMCI("stop all")
	p.sendMCI("close all")
	p.isPlaying = false
	p.currentFile = ""

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	// Escape backslashes for MCI or use forward slashes
	// In Windows MCI, wrapping absolute path in double quotes works reliably:
	openCmd := fmt.Sprintf(`open "%s" alias %s`, absPath, p.alias)
	if _, err := p.sendMCI(openCmd); err != nil {
		// Fallback without alias quotes or with specific type
		ext := strings.ToLower(filepath.Ext(absPath))
		if ext == ".mp3" {
			openCmd = fmt.Sprintf(`open "%s" type mpegvideo alias %s`, absPath, p.alias)
		} else if ext == ".wav" {
			openCmd = fmt.Sprintf(`open "%s" type waveaudio alias %s`, absPath, p.alias)
		}
		if _, err2 := p.sendMCI(openCmd); err2 != nil {
			return fmt.Errorf("gagal membuka audio: %w", err2)
		}
	}

	playCmd := fmt.Sprintf("play %s", p.alias)
	if _, err := p.sendMCI(playCmd); err != nil {
		p.sendMCI(fmt.Sprintf("close %s", p.alias))
		return fmt.Errorf("gagal memutar audio: %w", err)
	}

	p.currentFile = absPath
	p.isPlaying = true

	// Monitor playback end in a lightweight goroutine tied to currentID
	go func(id uint64, targetAlias string) {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			p.mu.Lock()
			if p.playID != id || !p.isPlaying {
				p.mu.Unlock()
				return
			}

			status, err := p.sendMCI(fmt.Sprintf("status %s mode", targetAlias))
			if err != nil || status != "playing" {
				if p.playID == id {
					p.isPlaying = false
					p.currentFile = ""
					p.sendMCI(fmt.Sprintf("close %s", targetAlias))
				}
				p.mu.Unlock()
				return
			}
			p.mu.Unlock()
		}
	}(currentID, p.alias)

	return nil
}

func (p *windowsPlayer) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.playID++
	p.isPlaying = false
	p.currentFile = ""
	p.sendMCI(fmt.Sprintf("stop %s", p.alias))
	p.sendMCI(fmt.Sprintf("close %s", p.alias))
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

