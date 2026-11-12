//go:build !linux && !darwin

package runshim

import "net"

// peerPIDSupported says this platform can tell which process is at the other end of a socket.
// shortcut: no peer check on systems other than Linux and macOS, add the system's call here
// when onett supports one.
const peerPIDSupported = false

func peerPID(*net.UnixConn) (int, error) { return 0, nil }
