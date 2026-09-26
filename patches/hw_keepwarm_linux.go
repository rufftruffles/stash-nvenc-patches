//go:build linux

package ffmpeg

import (
	"os/exec"
	"syscall"
)

// setParentDeathSignal makes the kernel kill cmd if Stash dies, so the CUDA
// keep-warm process is never left holding the GPU as an orphan.
func setParentDeathSignal(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
