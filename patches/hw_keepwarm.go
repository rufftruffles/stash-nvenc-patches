package ffmpeg

import (
	"context"
	"os/exec"
	"sync"
	"time"

	"github.com/stashapp/stash/pkg/logger"
)

const (
	// hwKeepWarmIdle is how long the holder process stays up after the last
	// generation job releases it.
	hwKeepWarmIdle = 60 * time.Second

	// hwKeepWarmMinUptime is how long the holder must survive after starting
	// for it to count as working. If it exits sooner, CUDA is assumed to be
	// unusable and keep-warm is disabled for the rest of the process.
	hwKeepWarmMinUptime = 2 * time.Second
)

// hwKeepWarm keeps a CUDA context open between short-lived ffmpeg processes.
//
// Without GPU persistence mode, the NVIDIA driver tears the GPU down whenever
// no process holds a context, and the next process reloads the GPU firmware.
// Measured on an RTX A2000 this added ~1.7-2s to every ffmpeg run, turning a
// ~0.47s preview segment into ~2.5s. Holding one idle context while generation
// runs keeps the driver initialised without needing root on the host.
type hwKeepWarm struct {
	mu       sync.Mutex
	users    int
	cmd      *exec.Cmd
	started  time.Time
	disabled bool
	idle     *time.Timer
	idleGen  int
}

var cudaKeepWarm hwKeepWarm

// HoldHWDevice keeps the hardware device for codec initialised until the
// returned release function is called, plus a short idle period so that
// back-to-back jobs share it. It only has an effect for NVENC codecs; for
// anything else it does nothing. The release function is safe to call more
// than once.
func (f *FFMpeg) HoldHWDevice(codec VideoCodec) (release func()) {
	switch codec {
	case VideoCodecN264, VideoCodecN264H:
		return cudaKeepWarm.acquire(f)
	}
	return func() {}
}

func (k *hwKeepWarm) acquire(f *FFMpeg) func() {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.users++
	if k.idle != nil {
		k.idle.Stop()
		k.idle = nil
	}
	if k.cmd == nil && !k.disabled {
		k.start(f)
	}

	var once sync.Once
	return func() { once.Do(k.release) }
}

func (k *hwKeepWarm) release() {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.users--
	if k.users > 0 || k.cmd == nil {
		return
	}

	// generation counter: a timer that already fired while a new job
	// acquired the holder must not stop it
	k.idleGen++
	gen := k.idleGen
	k.idle = time.AfterFunc(hwKeepWarmIdle, func() { k.stopIfIdle(gen) })
}

func (k *hwKeepWarm) stopIfIdle(gen int) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if gen != k.idleGen || k.users > 0 || k.cmd == nil {
		return
	}

	logger.Debugf("[ffmpeg] releasing CUDA keep-warm device after %s idle", hwKeepWarmIdle)
	_ = k.cmd.Process.Kill()
	k.cmd = nil
	k.idle = nil
}

// start launches the holder. Must be called with k.mu held.
func (k *hwKeepWarm) start(f *FFMpeg) {
	// -re throttles the null source to one tiny frame per second, so the
	// holder uses effectively no CPU or GPU while it keeps the context open.
	args := []string{
		"-hide_banner", "-nostdin", "-v", "error",
		"-init_hw_device", "cuda",
		"-re", "-f", "lavfi", "-i", "nullsrc=s=16x16:r=1",
		"-f", "null", "-",
	}
	cmd := f.Command(context.Background(), args)
	setParentDeathSignal(cmd)

	if err := cmd.Start(); err != nil {
		logger.Warnf("[ffmpeg] could not start CUDA keep-warm process, disabling it: %v", err)
		k.disabled = true
		return
	}

	k.cmd = cmd
	k.started = time.Now()
	logger.Debugf("[ffmpeg] keeping CUDA device initialised during generation (pid %d)", cmd.Process.Pid)

	go k.wait(cmd)
}

func (k *hwKeepWarm) wait(cmd *exec.Cmd) {
	err := cmd.Wait()

	k.mu.Lock()
	defer k.mu.Unlock()

	if k.cmd != cmd {
		// stopped on purpose by stopIfIdle
		return
	}
	k.cmd = nil

	if time.Since(k.started) < hwKeepWarmMinUptime {
		logger.Warnf("[ffmpeg] CUDA keep-warm process exited immediately, disabling it: %v", err)
		k.disabled = true
		return
	}
	// exited unexpectedly after running for a while; the next job restarts it
	logger.Debugf("[ffmpeg] CUDA keep-warm process exited: %v", err)
}
