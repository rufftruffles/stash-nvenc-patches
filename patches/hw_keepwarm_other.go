//go:build !linux

package ffmpeg

import "os/exec"

// setParentDeathSignal is a no-op where the kernel has no parent-death signal.
func setParentDeathSignal(cmd *exec.Cmd) {}
