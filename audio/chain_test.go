package audio

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fakePlayer records played files and lets the test simulate a natural end.
type fakePlayer struct {
	mu     sync.Mutex
	played []string
	onEnd  func()
}

func (f *fakePlayer) Play(p string) error {
	f.mu.Lock()
	f.played = append(f.played, filepath.Base(p))
	f.mu.Unlock()
	return nil
}
func (f *fakePlayer) Stop() error         { return nil }
func (f *fakePlayer) IsPlaying() bool     { return true }
func (f *fakePlayer) CurrentFile() string { return "" }
func (f *fakePlayer) SetOnEnd(fn func())  { f.onEnd = fn }
func (f *fakePlayer) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.played...)
}

func TestFollowUpChainPlaysInOrder(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"bel.mp3", "raya.mp3", "daerah.mp3"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	fp := &fakePlayer{}
	m := newManagerWithPlayer(dir, fp)

	var started []string
	m.SetOnStart(func(code, name string) { started = append(started, code) })

	err := m.PlayFileThen(filepath.Join(dir, "bel.mp3"), "AUD-18", "Istirahat",
		QueueItem{Path: filepath.Join(dir, "raya.mp3"), Code: "AUD-16", Name: "Raya"},
		QueueItem{Path: filepath.Join(dir, "daerah.mp3"), Code: "FLD-01", Name: "Daerah"},
	)
	if err != nil {
		t.Fatal(err)
	}
	fp.onEnd() // bell finished
	fp.onEnd() // Indonesia Raya finished
	fp.onEnd() // lagu daerah finished -> nothing more

	got := fp.list()
	want := []string{"bel.mp3", "raya.mp3", "daerah.mp3"}
	if len(got) != len(want) {
		t.Fatalf("played %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("played %v, want %v", got, want)
		}
	}
	if len(started) != 2 || started[0] != "AUD-16" || started[1] != "FLD-01" {
		t.Fatalf("onStart %v", started)
	}
}

func TestStopCancelsFollowUpChain(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"bel.mp3", "raya.mp3"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	fp := &fakePlayer{}
	m := newManagerWithPlayer(dir, fp)

	_ = m.PlayFileThen(filepath.Join(dir, "bel.mp3"), "AUD-18", "Istirahat",
		QueueItem{Path: filepath.Join(dir, "raya.mp3"), Code: "AUD-16", Name: "Raya"})
	_ = m.Stop()
	fp.onEnd()

	if got := fp.list(); len(got) != 1 {
		t.Fatalf("follow-up should be cancelled after Stop, played %v", got)
	}
}
