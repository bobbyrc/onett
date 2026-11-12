package runshim

import (
	"net"
	"syscall"
)

// peerPIDSupported says this platform can tell which process is at the other end of a socket.
const peerPIDSupported = true

// From <sys/un.h>; package syscall does not define them.
const (
	solLocal     = 0     // SOL_LOCAL
	localPeerPID = 0x002 // LOCAL_PEERPID
)

// peerPID returns the process ID at the other end of a Unix socket connection, as the kernel
// recorded it (LOCAL_PEERPID). The peer cannot choose it.
func peerPID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var serr error
	if err := raw.Control(func(fd uintptr) {
		pid, serr = syscall.GetsockoptInt(int(fd), solLocal, localPeerPID)
	}); err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, serr
	}
	return pid, nil
}
