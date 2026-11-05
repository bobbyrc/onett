package claude

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

const (
	maxInputChars  = 500  // tool_use input shown in the log
	maxResultChars = 2000 // tool_result content shown in the log
	maxRawChars    = 500  // unrecognised lines shown in the log
)

// streamLine is the subset of Claude Code's stream-json lines that onett reads. Each line has
// a "type": "system" (init, hooks), "assistant", "user", "result" or "rate_limit_event".
type streamLine struct {
	Type              string          `json:"type"`
	Subtype           string          `json:"subtype"`
	SessionID         string          `json:"session_id"`
	Model             string          `json:"model"`
	APIKeySource      string          `json:"apiKeySource"`
	Message           json.RawMessage `json:"message"`
	Result            string          `json:"result"`
	IsError           bool            `json:"is_error"`
	TotalCostUSD      float64         `json:"total_cost_usd"`
	Usage             *usage          `json:"usage"`
	PermissionDenials []denial        `json:"permission_denials"`
	APIErrorStatus    int             `json:"api_error_status"`
	RateLimitInfo     *rateLimitInfo  `json:"rate_limit_info"`
}

// rateLimitInfo is the payload of a rate_limit_event. Status is "allowed", "allowed_warning" or
// "rejected"; only a rejection means the limit was hit. ResetsAt is epoch seconds.
type rateLimitInfo struct {
	Status   string `json:"status"`
	ResetsAt int64  `json:"resetsAt"`
}

type usage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

type message struct {
	Content json.RawMessage `json:"content"`
}

// block is one content block of an assistant or user message.
type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	ToolUseID string          `json:"tool_use_id"`
}

// denial is one entry of the result line's permission_denials.
type denial struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

// parser turns stream-json lines into events and keeps what the final Result needs.
type parser struct {
	model     string // requested model, used in the init message when the line has none
	sessionID string // latest session ID seen on any line
	announced bool   // whether sessionID has been attached to an emitted event
	lastText  string // last assistant text block
	billing   string // from the init line's apiKeySource; "" until seen
	denials   []string
	// rejected is the last rate_limit_event that said the limit was hit (nil if none).
	rejected *rateLimitInfo
	// now is the clock for reset times that are only a time of day; time.Now unless a test sets it.
	now   func() time.Time
	final *streamLine // the last result line seen
	// inputTokens and outputTokens sum the per-turn usage of every result line: with stream-json
	// input one process can answer several turns, and each result reports only its own turn.
	inputTokens  int64
	outputTokens int64
	// onResult, if set, runs after each result line is parsed.
	onResult func()
}

func newParser(model string) *parser {
	return &parser{model: model, now: time.Now}
}

// parseLine handles one stdout line. It never fails: anything it does not understand becomes a
// system event with the raw line.
func (p *parser) parseLine(line string) []harness.Event {
	var sl streamLine
	if err := json.Unmarshal([]byte(line), &sl); err != nil || sl.Type == "" {
		return p.attach([]harness.Event{event(domain.LogSystem, harness.Truncate(line, maxRawChars))})
	}
	if sl.SessionID != "" && sl.SessionID != p.sessionID {
		p.sessionID = sl.SessionID
		p.announced = false
	}

	var events []harness.Event
	switch sl.Type {
	case "system":
		// Only init is kept. Hook and informational lines are environment noise.
		if sl.Subtype == "init" {
			p.billing = billingFor(sl.APIKeySource)
			model := sl.Model
			if model == "" {
				model = p.model
			}
			text := "session started"
			if model != "" {
				text = fmt.Sprintf("session started (model %s)", model)
			}
			events = append(events, event(domain.LogSystem, text))
		}
	case "assistant":
		events = p.assistantEvents(sl.Message)
	case "user":
		events = toolResultEvents(sl.Message)
	case "result":
		events = p.resultEvents(sl)
	case "rate_limit_event":
		// Emitted on every turn; not useful in the log. Only a rejection matters: it is kept so
		// the result can say the run stopped on the limit.
		if ri := sl.RateLimitInfo; ri != nil && ri.Status == "rejected" {
			p.rejected = ri
		}
	default:
		events = append(events, event(domain.LogSystem, harness.Truncate(line, maxRawChars)))
	}
	return p.attach(events)
}

// attach puts the session ID on the first emitted event after it is first seen.
func (p *parser) attach(events []harness.Event) []harness.Event {
	if len(events) > 0 && !p.announced && p.sessionID != "" {
		events[0].SessionID = p.sessionID
		p.announced = true
	}
	return events
}

func (p *parser) assistantEvents(raw json.RawMessage) []harness.Event {
	var msg message
	if json.Unmarshal(raw, &msg) != nil {
		return nil
	}
	var events []harness.Event
	for _, b := range parseBlocks(msg.Content) {
		switch b.Type {
		case "text":
			if b.Text == "" {
				continue
			}
			p.lastText = b.Text
			events = append(events, event(domain.LogText, b.Text))
		case "tool_use":
			text := b.Name + ": " + harness.CompactJSON(b.Input, maxInputChars)
			ev := event(domain.LogToolUse, text)
			ev.ToolID = b.ID
			events = append(events, ev)
		}
	}
	return events
}

func toolResultEvents(raw json.RawMessage) []harness.Event {
	var msg message
	if json.Unmarshal(raw, &msg) != nil {
		return nil
	}
	var events []harness.Event
	for _, b := range parseBlocks(msg.Content) {
		if b.Type != "tool_result" {
			continue
		}
		text := harness.Truncate(contentText(b.Content), maxResultChars)
		if b.IsError {
			text = "error: " + text
		}
		ev := event(domain.LogToolResult, text)
		ev.ToolID = b.ToolUseID
		events = append(events, ev)
	}
	return events
}

func (p *parser) resultEvents(sl streamLine) []harness.Event {
	final := sl
	p.final = &final
	if sl.Usage != nil {
		p.inputTokens += sl.Usage.InputTokens + sl.Usage.CacheCreationInputTokens + sl.Usage.CacheReadInputTokens
		p.outputTokens += sl.Usage.OutputTokens
	}
	if p.onResult != nil {
		defer p.onResult()
	}
	var events []harness.Event
	for _, d := range sl.PermissionDenials {
		name := d.ToolName
		if name == "" {
			name = "tool"
		}
		text := name + ": " + harness.CompactJSON(d.ToolInput, maxInputChars)
		p.denials = append(p.denials, text)
		events = append(events, event(domain.LogDenial, text))
	}
	if sl.IsError {
		events = append(events, event(domain.LogError, harness.Truncate(sl.Result, maxResultChars)))
	}
	return events
}

// result builds the Result once the process has exited. It needs the exit code and the last
// stderr lines, which are the only explanation when the CLI dies without a result line.
func (p *parser) result(exitCode int, stderrTail []string) harness.Result {
	res := harness.Result{
		SessionID: p.sessionID,
		ExitCode:  exitCode,
		Denials:   p.denials,
		FinalText: p.lastText,
		Billing:   p.billing,
	}
	if f := p.final; f != nil {
		res.IsError = f.IsError
		res.CostUSD = f.TotalCostUSD
		res.InputTokens = p.inputTokens
		res.OutputTokens = p.outputTokens
		if f.Result != "" {
			res.FinalText = f.Result
		}
	} else if exitCode != 0 && len(stderrTail) > 0 {
		res.FinalText = strings.Join(stderrTail, "\n")
	}
	if exitCode != 0 {
		res.IsError = true
	}
	res.RateLimit = p.rateLimit(res)
	return res
}

// rateLimit reports the limit that ended the run, or nil. The run must have ended in error: a
// warning or an "allowed" event during a run that completed is not a hit. Claude Code signals a
// hit with a rejected rate_limit_event, an API status of 429, or result text such as "Claude AI
// usage limit reached|<epoch>" or "You've hit your limit · resets 3pm".
func (p *parser) rateLimit(res harness.Result) *harness.RateLimit {
	if !res.IsError {
		return nil
	}
	text := res.FinalText
	hit := p.rejected != nil || harness.LooksRateLimited(text)
	if f := p.final; f != nil && f.APIErrorStatus == 429 {
		hit = true
	}
	if !hit {
		return nil
	}
	now := p.now()
	rl := &harness.RateLimit{Message: harness.Truncate(text, maxResultChars)}
	rl.ResetAt = harness.ResetFromText(text, now)
	if rl.ResetAt.IsZero() && p.rejected != nil && p.rejected.ResetsAt > 0 {
		if at := time.Unix(p.rejected.ResetsAt, 0); at.After(now) {
			rl.ResetAt = at
		}
	}
	if rl.Message == "" {
		rl.Message = "usage limit reached"
	}
	return rl
}

// billingFor maps the init line's apiKeySource to a billing mode. "none" means Claude Code is
// signed in with a subscription; any other source is an API key, which is billed per token.
func billingFor(apiKeySource string) string {
	switch apiKeySource {
	case "":
		return ""
	case "none":
		return harness.BillingSubscription
	default:
		return harness.BillingMetered
	}
}

func event(kind domain.LogKind, text string) harness.Event {
	return harness.Event{Kind: kind, Text: text}
}

// parseBlocks decodes a message content field, which is an array of blocks. A plain string or
// anything else yields no blocks.
func parseBlocks(raw json.RawMessage) []block {
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	return blocks
}

// contentText flattens a tool_result content field, which is either a string or an array of
// text blocks.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []string
	for _, b := range parseBlocks(raw) {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}
