package main

import (
	"fmt"
	"syscall"
)

// supervisedByLaunchd reports whether getenv says launchd started this process as the onett
// service (`onett service install`). launchd sets XPC_SERVICE_NAME to the job's label.
func supervisedByLaunchd(getenv func(string) string) bool {
	return getenv("XPC_SERVICE_NAME") == serviceLabel
}

// execSelf replaces the process with a fresh copy of the binary: same arguments, same
// environment. It returns only on failure. It is a variable so tests do not exec.
var execSelf = func(exe string, args, env []string) error {
	return syscall.Exec(exe, args, env)
}

// restartAfterShutdown finishes a restart requested through POST /restart, after the normal
// shutdown has stopped the server and the engine and closed the store. Under launchd it returns
// nil so serve exits and KeepAlive starts a new process (about ThrottleInterval later).
// Otherwise nothing would start us again, so it re-executes the binary in place.
func restartAfterShutdown(exe string, args, env []string, getenv func(string) string) error {
	if supervisedByLaunchd(getenv) {
		return nil
	}
	if err := execSelf(exe, args, env); err != nil {
		return fmt.Errorf("restart: re-exec %s: %w", exe, err)
	}
	return nil
}
