// Package events defines task lifecycle event types and dispatch helpers
// used by both the webhook emitter and the audit log writer.
package events

import "time"

// Type is the lifecycle event type.
type Type string

const (
	TypeTaskCreated   Type = "task.created"
	TypeTaskStarted   Type = "task.started"
	TypeTaskOutput    Type = "task.output"
	TypeTaskCompleted Type = "task.completed"
	TypeTaskFailed    Type = "task.failed"
	TypeVMCreated     Type = "vm.created"
	TypeVMDestroyed   Type = "vm.destroyed"
)

// Event is the wire format for both webhooks and audit log entries.
type Event struct {
	ID        string                 `json:"id"`
	Type      Type                   `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	TaskID    string                 `json:"task_id,omitempty"`
	VMName    string                 `json:"vm_name,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// Sink receives events. Implementations include webhook.Sender and audit.Logger.
type Sink interface {
	Emit(ev Event)
}

// Multi dispatches to several sinks. A nil sink is skipped so callers can
// build a Multi unconditionally.
type Multi []Sink

// Emit implements Sink.
func (m Multi) Emit(ev Event) {
	for _, s := range m {
		if s != nil {
			s.Emit(ev)
		}
	}
}
