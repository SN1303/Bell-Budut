//go:build !windows

package audio

import "errors"

type otherPlayer struct{}

func newPlatformPlayer() Player {
	return &otherPlayer{}
}

func (p *otherPlayer) Play(filePath string) error {
	return errors.New("audio player sistem saat ini dioptimalkan untuk Windows (MCI)")
}

func (p *otherPlayer) Stop() error {
	return nil
}

func (p *otherPlayer) IsPlaying() bool {
	return false
}

func (p *otherPlayer) CurrentFile() string {
	return ""
}

