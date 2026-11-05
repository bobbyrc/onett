// Package runshim is a small per-run supervisor process, the containerd-shim / conmon pattern.
// One shim owns one agent CLI process and its pipes. It keeps draining the agent's stdout into a
// log file whether or not the daemon is connected, so the daemon can exit and come back without
// killing the run and without the agent ever blocking on a full pipe.
//
// The shim is the `onett run-shim` subcommand. The daemon starts it detached (Start), then talks
// to it over a Unix socket with a versioned, line-delimited JSON protocol (Client):
//
//	hello        the client sends its protocol version; a different version is refused
//	replay       send the stdout lines from a byte offset, then follow new ones live
//	write_stdin  write bytes to the agent's stdin (serialised by the client)
//	close_stdin  close the agent's stdin
//	signal       send SIGTERM or SIGKILL to the agent's whole process group
//	status       return the shim's Status
//	ack          the client has processed the log up to an offset
//
// Files, all under <data_dir>/runs/<run_id>/: stream.log (every stdout line; a line's offset is
// its byte position in this file), status.json (a Status, replaced atomically) and, while the
// daemon waits for the shim, spec.json. The socket is <data_dir>/s/<8 chars>.sock, short because
// macOS limits socket paths to about 104 bytes; status.json records where it is.
//
// The shim exits when the agent has exited and a client has acked the final offset, or when a
// grace period after the agent's exit passes with no such ack.
package runshim

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ProtocolVersion is the socket protocol version. A shim refuses a client that speaks another.
const ProtocolVersion = 1

const (
	// DefaultGrace is how long a shim outlives its agent when no client acks the final offset.
	DefaultGrace = 24 * time.Hour
	// MaxLine caps one stdout line; longer lines are cut at this size.
	MaxLine = 10 << 20
	// StderrTailLines is how many stderr lines the shim keeps.
	StderrTailLines = 20
	// maxSocketPath is the longest socket path used; macOS allows about 104 bytes.
	maxSocketPath = 100

	streamLogName = "stream.log"
	statusName    = "status.json"
	specName      = "spec.json"
	shimLogName   = "shim.log"
)

// Spec is everything a shim needs to start one agent. The daemon writes it to a file whose path
// is the shim's only argument, so a long command line or the environment never goes through argv.
type Spec struct {
	RunID string `json:"run_id"`
	// RunDir holds stream.log and status.json.
	RunDir     string `json:"run_dir"`
	SocketPath string `json:"socket_path"`

	Command []string `json:"command"`
	Dir     string   `json:"dir,omitempty"`
	// Env is the complete agent environment as KEY=VALUE entries; the shim adds nothing to it.
	Env []string `json:"env,omitempty"`
	// Stdin is written to the agent and then, unless KeepStdinOpen, closed.
	Stdin         string `json:"stdin,omitempty"`
	KeepStdinOpen bool   `json:"keep_stdin_open,omitempty"`

	// Grace is how long the shim outlives the agent without a final ack (0 = DefaultGrace).
	Grace time.Duration `json:"grace_ns,omitempty"`
}

// Status is what a shim knows about its run. It is the content of status.json and the answer to
// the status request.
type Status struct {
	RunID        string     `json:"run_id"`
	Version      int        `json:"version"`
	ShimPID      int        `json:"shim_pid"`
	AgentPID     int        `json:"agent_pid"`
	AgentPGID    int        `json:"agent_pgid"`
	Socket       string     `json:"socket"`
	StartedAt    time.Time  `json:"started_at"`
	LastOutputAt time.Time  `json:"last_output_at"`
	Exited       bool       `json:"exited"`
	ExitCode     int        `json:"exit_code"`
	ExitedAt     *time.Time `json:"exited_at,omitempty"`
	// LogSize is the size of stream.log in bytes; Acked is the highest offset a client acked.
	LogSize    int64    `json:"log_size"`
	Acked      int64    `json:"acked"`
	StderrTail []string `json:"stderr_tail,omitempty"`
	// StartError is set when the agent could not be started; the shim then exits at once.
	StartError string `json:"start_error,omitempty"`
}

// RunDir returns the directory holding a run's shim files.
func RunDir(dataDir, runID string) string {
	return filepath.Join(dataDir, "runs", runID)
}

// StatusPath returns the status.json path inside runDir.
func StatusPath(runDir string) string { return filepath.Join(runDir, statusName) }

// StreamPath returns the stream.log path inside runDir.
func StreamPath(runDir string) string { return filepath.Join(runDir, streamLogName) }

// ReadStatus reads the status.json in runDir.
func ReadStatus(runDir string) (Status, error) {
	var st Status
	b, err := os.ReadFile(StatusPath(runDir))
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, fmt.Errorf("read %s: %w", StatusPath(runDir), err)
	}
	return st, nil
}

// SocketPath returns the socket path for a run: <data_dir>/s/<8 chars>.sock, with the 8 characters
// taken from a hash of the run ID. A data_dir so long that this would pass the socket path limit
// falls back to /tmp/onett-<uid>/s/. The directory is created, mode 0700.
func SocketPath(dataDir, runID string) (string, error) {
	sum := sha256.Sum256([]byte(runID))
	name := hex.EncodeToString(sum[:])[:8] + ".sock"
	dir := filepath.Join(dataDir, "s")
	if len(filepath.Join(dir, name)) > maxSocketPath {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("onett-%d", os.Getuid()), "s")
		if len(filepath.Join(dir, name)) > maxSocketPath {
			dir = filepath.Join("/tmp", fmt.Sprintf("onett-%d", os.Getuid()), "s")
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create socket dir: %w", err)
	}
	// MkdirAll leaves an existing directory alone, so check we own it and nobody else can enter.
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("socket dir %s must be a private directory (mode 0700)", dir)
	}
	return filepath.Join(dir, name), nil
}
