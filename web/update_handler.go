package web

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"bell-budut/updater"
	"bell-budut/version"
)

var (
	restartMu sync.Mutex
	restartFn func()
)

// SetRestartFunc registers how the app restarts itself after an OTA update
// (main starts the new exe and shuts the current process down).
func (s *Server) SetRestartFunc(fn func()) {
	restartMu.Lock()
	restartFn = fn
	restartMu.Unlock()
}

// GET /api/version
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	s.jsonSuccess(w, map[string]interface{}{
		"version": version.Version,
		"repo":    version.Repo,
	})
}

// GET /api/update/check[?force=1]
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	rel, err := updater.Check(ctx, r.URL.Query().Get("force") == "1")
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.jsonSuccess(w, map[string]interface{}{
		"current":    version.Version,
		"latest":     rel.Version,
		"has_update": updater.IsNewer(rel.Version, version.Version) && rel.AssetURL != "",
		"release":    rel,
	})
}

// POST /api/update/apply (admin only)
func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Metode tidak valid", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	rel, err := updater.Check(ctx, true)
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	if !updater.IsNewer(rel.Version, version.Version) {
		s.jsonError(w, "Aplikasi sudah versi terbaru ("+version.Version+")", http.StatusBadRequest)
		return
	}

	if err := updater.Apply(ctx, rel); err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"type":    "APP_UPDATING",
		"version": rel.Version,
	})
	s.sched.Broadcast(payload)

	s.jsonSuccess(w, map[string]interface{}{
		"message": "Update ke versi " + rel.Version + " terpasang. Aplikasi akan restart otomatis...",
		"version": rel.Version,
	})

	// Restart shortly after the response has been delivered
	go func() {
		time.Sleep(1500 * time.Millisecond)
		restartMu.Lock()
		fn := restartFn
		restartMu.Unlock()
		if fn != nil {
			fn()
		}
	}()
}
