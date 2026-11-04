//go:build !unix

package ytdlp

import "os/exec"

func setProcessGroup(*exec.Cmd) {}
