package runshim

import (
	"net"
	"syscall"
)

// peerPIDSupported says this platform can tell which process is at the other end of a socket.
const peerPIDSupported = true

// peerPID returns the process ID at the other end of a Unix socket connection, as the kernel
// recorded it (SO_PEERCRED). The peer cannot choose it.
func peerPID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var serr error
	if err := raw.Control(func(fd uintptr) {
		cred, serr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, serr
	}
	return int(cred.Pid), nil
}
