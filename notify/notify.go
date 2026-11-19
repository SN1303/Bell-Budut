// Package notify shows a small always-on-top popup in the bottom-right corner of
// the Windows desktop while audio plays on the server speaker.
//
// The popup does not depend on Windows notification settings (which are often
// turned off on school PCs). It is a tiny PowerShell/WinForms window that:
//   - polls GET /api/audio/now every second and shows what is playing
//   - has a "Hentikan Audio" button (POST /api/audio/stop-server)
//   - closes itself shortly after playback ends
//   - never steals keyboard focus from the operator
package notify

import (
	"sync"
)

var (
	mu      sync.Mutex
	running bool
)

// ShowPopup opens the "now playing" popup if it is not already open.
func ShowPopup(port int) {
	mu.Lock()
	if running {
		mu.Unlock()
		return
	}
	running = true
	mu.Unlock()

	go func() {
		_ = runPopup(port) // blocks until the popup closes itself
		mu.Lock()
		running = false
		mu.Unlock()
	}()
}
