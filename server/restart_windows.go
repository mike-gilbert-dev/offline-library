package main

import (
	"os"
	"os/exec"
)

// restartSelf starts the (newly installed) launcher and returns so this
// process can exit. Windows can't replace a running process image, so the
// new one is a child that shares this console window; the window stays open
// after this process exits because the child is still attached to it.
func restartSelf(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Start()
}
