//go:build !windows

package main

import (
	"os"
	"syscall"
)

// restartSelf replaces this process with the newly installed launcher. Using
// exec (same process, same Terminal window) avoids the new launcher being
// killed when Terminal closes the old process's session. Listening sockets
// are closed on exec, so the new launcher can bind its ports.
func restartSelf(exe string, args []string) error {
	return syscall.Exec(exe, append([]string{exe}, args...), os.Environ())
}
