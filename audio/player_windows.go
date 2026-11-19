//go:build windows

package audio

import (
	"fmt"
	"path/filepath"
	"runtime"
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

// ==================== DEDICATED MCI THREAD ====================
// Windows MCI device aliases are bound to the OS thread that opened them.
// Go goroutines (HTTP handlers, scheduler, monitor) hop between OS threads,
// so "stop"/"close" issued from another request used to fail silently with
// MCI error 263 -> the old song kept playing and could not be stopped.
// All MCI commands are therefore executed on ONE locked OS thread.

type mciRequest struct {
	command string
	resp    chan mciResponse
}

type mciResponse struct {
	out string
	err error
}

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	peekMessageW     = user32.NewProc("PeekMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	dispatchMessageW = user32.NewProc("DispatchMessageW")

	mciOnce  sync.Once
	mciQueue chan mciRequest
)

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
	private uint32
}

func startMCIThread() {
	mciQueue = make(chan mciRequest)
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread() // never unlocked: this goroutine owns the MCI thread forever
		close(ready)
		pump := time.NewTicker(30 * time.Millisecond)
		defer pump.Stop()
		for {
			select {
			case req := <-mciQueue:
				out, err := rawMCI(req.command)
				req.resp <- mciResponse{out: out, err: err}
			case <-pump.C:
				pumpMessages()
			}
		}
	}()
	<-ready
}

// pumpMessages drains the thread's window messages (MCI/DirectShow hidden windows).
func pumpMessages() {
	var msg winMsg
	for {
		r, _, _ := peekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1 /* PM_REMOVE */)
		if r == 0 {
			return
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func rawMCI(command string) (string, error) {
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

func (p *windowsPlayer) sendMCI(command string) (string, error) {
	mciOnce.Do(startMCIThread)
	resp := make(chan mciResponse, 1)
	mciQueue <- mciRequest{command: command, resp: resp}
	r := <-resp
	return r.out, r.err
}

func (p *windowsPlayer) Play(filePath string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.playID++
	currentID := p.playID

	// 1. Cleanly stop and close the previous alias if still active
	if p.currentAlias != "" {
		p.sendMCI(fmt.Sprintf("stop %s", p.currentAlias))
		p.sendMCI(fmt.Sprintf("close %s", p.currentAlias))
		p.currentAlias = ""
	}
	p.isPlaying = false
	p.currentFile = ""

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	var typeStr string
	if ext == ".mp3" {
		typeStr = " type mpegvideo"
	} else if ext == ".wav" {
		typeStr = " type waveaudio"
	}

	// Dynamic cyclic alias prevents collisions with previous DirectShow teardown
	targetAlias := fmt.Sprintf("bell_dev_%d", currentID)
	openCmd := fmt.Sprintf(`open "%s"%s alias %s`, absPath, typeStr, targetAlias)

	var lastErr error
	opened := false
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(50 * time.Millisecond)
		}
		if _, err := p.sendMCI(openCmd); err != nil {
			// Fallback without explicit type specifier
			openFallback := fmt.Sprintf(`open "%s" alias %s`, absPath, targetAlias)
			if _, err2 := p.sendMCI(openFallback); err2 != nil {
				lastErr = err2
				continue
			}
		}
		opened = true
		break
	}

	if !opened {
		return fmt.Errorf("gagal membuka audio (%s): %w", absPath, lastErr)
	}

	// 2. Query duration in milliseconds
	p.sendMCI(fmt.Sprintf("set %s time format milliseconds", targetAlias))
	lenStr, _ := p.sendMCI(fmt.Sprintf("status %s length", targetAlias))
	durMs, _ := strconv.ParseInt(strings.TrimSpace(lenStr), 10, 64)
	p.durationMs = durMs
	p.startTime = time.Now()
	p.currentAlias = targetAlias

	playCmd := fmt.Sprintf("play %s", targetAlias)
	played := false
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(50 * time.Millisecond)
		}
		if _, err := p.sendMCI(playCmd); err != nil {
			lastErr = err
			continue
		}
		played = true
		break
	}

	if !played {
		p.sendMCI(fmt.Sprintf("close %s", targetAlias))
		p.currentAlias = ""
		return fmt.Errorf("gagal memutar audio: %w", lastErr)
	}

	p.currentFile = absPath
	p.isPlaying = true

	// 3. Monitor playback end in a background goroutine with duration awareness
	go func(id uint64, alias string, durationMs int64, startTime time.Time) {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()

		consecutiveStopped := 0

		for range ticker.C {
			p.mu.Lock()
			if p.playID != id || !p.isPlaying {
				p.mu.Unlock()
				return
			}

			elapsedMs := time.Since(startTime).Milliseconds()

			// Safety: Never trigger finish within first 800ms
			if elapsedMs < 800 {
				p.mu.Unlock()
				continue
			}

			// While elapsed time is well within known duration, audio is still playing
			if durationMs > 1500 && elapsedMs < durationMs-400 {
				p.mu.Unlock()
				continue
			}

			status, _ := p.sendMCI(fmt.Sprintf("status %s mode", alias))
			posStr, _ := p.sendMCI(fmt.Sprintf("status %s position", alias))
			posMs, _ := strconv.ParseInt(strings.TrimSpace(posStr), 10, 64)

			isFinished := false
			if durationMs > 0 {
				if (posMs > 0 && posMs >= durationMs-200) || elapsedMs >= durationMs+1500 {
					isFinished = true
				}
			}

			if status == "stopped" {
				consecutiveStopped++
				if consecutiveStopped >= 2 {
					isFinished = true
				}
			} else if status == "playing" {
				consecutiveStopped = 0
			}

			if isFinished {
				if p.playID == id {
					p.isPlaying = false
					p.currentFile = ""
					p.sendMCI(fmt.Sprintf("stop %s", alias))
					p.sendMCI(fmt.Sprintf("close %s", alias))
					if p.currentAlias == alias {
						p.currentAlias = ""
					}
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
	}(currentID, targetAlias, durMs, p.startTime)

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
