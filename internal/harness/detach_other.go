//go:build !unix

package harness

import "os/exec"

func detach(cmd *exec.Cmd) {}
