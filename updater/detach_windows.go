//go:build windows

package updater

import (
	"os/exec"
	"syscall"
)

func setDetached(cmd *exec.Cmd) {
	const detachedProcess = 0x00000008
	const createNewProcessGroup = 0x00000200
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
		HideWindow:    true,
	}
}
