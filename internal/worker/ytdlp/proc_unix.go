//go:build unix

package ytdlp

import (
	"os/exec"
	"syscall"
)

// setProcessGroup makes cancellation kill yt-dlp together with the ffmpeg it
// spawns for merging.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
