// Package bridge provides host-side execution of bridge-type plugins
// that run as external subprocesses (Python, Node, etc.) with an NDJSON
// stdout protocol for progress, artifacts, and errors.
package bridge

import (
	"encoding/json"
	"fmt"
)

// EventType categorises an NDJSON line from the subprocess.
type EventType string

const (
	EventProgress EventType = "progress"
	EventLog      EventType = "log"
	EventCaptcha  EventType = "captcha"
	EventArtifact EventType = "artifact"
	EventDone     EventType = "done"
	EventError    EventType = "error"
)

// Event is a raw NDJSON line read from the subprocess stdout.
type Event struct {
	Type EventType       `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

// Progress reports per-attempt advancement.
type Progress struct {
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	Email    string `json:"email,omitempty"`
	Username string `json:"username,omitempty"`
}

// Log is a free-form log line.
type Log struct {
	Msg string `json:"msg"`
}

// Captcha reports solver status.
type Captcha struct {
	Status   string `json:"status"`
	Platform string `json:"platform,omitempty"`
	TaskID   string `json:"taskId,omitempty"`
}

// Artifact signals a completed output file ready for ingestion.
type Artifact struct {
	Kind     string `json:"kind"`
	File     string `json:"file"`
	Email    string `json:"email,omitempty"`
	Username string `json:"username,omitempty"`
}

// Done is the terminal event (process may still exit non-zero).
type Done struct {
	OK    int `json:"ok"`
	Fail  int `json:"fail"`
	Total int `json:"total"`
}

// Error is a non-fatal per-attempt failure.
type Error struct {
	Attempt int    `json:"attempt"`
	Msg     string `json:"msg"`
	Email   string `json:"email,omitempty"`
}

// Parse reads a raw JSON line and returns a typed Event.
func Parse(line []byte) (Event, error) {
	var ev struct {
		Type EventType `json:"type"`
	}
	if err := json.Unmarshal(line, &ev); err != nil {
		return Event{}, fmt.Errorf("bridge: parse event type: %w", err)
	}
	return Event{Type: ev.Type, Raw: line}, nil
}

// Unmarshal parses the event payload into the appropriate struct.
func (e Event) Unmarshal() (any, error) {
	switch e.Type {
	case EventProgress:
		var v Progress
		return &v, json.Unmarshal(e.Raw, &v)
	case EventLog:
		var v Log
		return &v, json.Unmarshal(e.Raw, &v)
	case EventCaptcha:
		var v Captcha
		return &v, json.Unmarshal(e.Raw, &v)
	case EventArtifact:
		var v Artifact
		return &v, json.Unmarshal(e.Raw, &v)
	case EventDone:
		var v Done
		return &v, json.Unmarshal(e.Raw, &v)
	case EventError:
		var v Error
		return &v, json.Unmarshal(e.Raw, &v)
	default:
		return nil, fmt.Errorf("bridge: unknown event type %q", e.Type)
	}
}
