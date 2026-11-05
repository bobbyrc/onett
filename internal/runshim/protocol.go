package runshim

// Wire format: one JSON object per line in each direction. A client sends requests; the shim
// answers each with a "response" carrying the same ID. A replay request additionally makes the
// shim push "line", "stderr" and "eof" messages on that connection.

// Request ops.
const (
	opHello      = "hello"
	opReplay     = "replay"
	opWriteStdin = "write_stdin"
	opCloseStdin = "close_stdin"
	opSignal     = "signal"
	opStatus     = "status"
	opAck        = "ack"
)

// Message types sent by the shim.
const (
	msgResponse = "response"
	// MsgLine is one stdout line: Offset is where it starts in stream.log, Next where the next
	// line starts. Persist Next to resume after this line.
	MsgLine = "line"
	// MsgStderr is one live stderr line. Stderr is not logged, so a replay never repeats it.
	MsgStderr = "stderr"
	// MsgEOF ends the stream: the agent has exited and every line was sent. Offset is the final
	// log size, the offset to ack; ExitCode is the agent's.
	MsgEOF = "eof"
)

type request struct {
	ID      int64  `json:"id"`
	Op      string `json:"op"`
	Version int    `json:"version,omitempty"`
	From    int64  `json:"from,omitempty"`
	Data    []byte `json:"data,omitempty"`
	Signal  string `json:"signal,omitempty"`
	Offset  int64  `json:"offset,omitempty"`
}

// message is everything the shim sends.
type message struct {
	Type string `json:"type"`
	ID   int64  `json:"id,omitempty"`

	// response
	OK      bool    `json:"ok,omitempty"`
	Error   string  `json:"error,omitempty"`
	Code    string  `json:"code,omitempty"`
	Version int     `json:"version,omitempty"`
	Status  *Status `json:"status,omitempty"`

	// line, stderr, eof
	Offset   int64  `json:"offset,omitempty"`
	Next     int64  `json:"next,omitempty"`
	Text     string `json:"text,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
}

// Error codes in a response, so a client can tell these apart from a generic failure.
const (
	codeVersion     = "version_mismatch"
	codeStdinClosed = "stdin_closed"
)
