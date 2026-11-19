//go:build !windows

package updater

import "os/exec"

func setDetached(cmd *exec.Cmd) {}
