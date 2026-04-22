package events

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
)

// AuditLogger appends JSON-lines audit entries to a file.
//
// Intended for compliance/forensics: every task's prompt, outcome, and
// destruction time. The file is opened append-only with 0600 permissions;
// consumers should rotate externally (logrotate, journald).
type AuditLogger struct {
	mu  sync.Mutex
	f   *os.File
	log *slog.Logger
}

// NewAuditLogger opens path (creating it if missing). Returns nil if path is empty.
func NewAuditLogger(path string, log *slog.Logger) (*AuditLogger, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	return &AuditLogger{f: f, log: log}, nil
}

// Emit implements Sink.
func (a *AuditLogger) Emit(ev Event) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	data, err := json.Marshal(ev)
	if err != nil {
		a.log.Warn("audit marshal", "error", err)
		return
	}
	if _, err := a.f.Write(append(data, '\n')); err != nil {
		a.log.Warn("audit write", "error", err)
	}
}

// Close releases the underlying file handle.
func (a *AuditLogger) Close() error {
	if a == nil {
		return nil
	}
	return a.f.Close()
}
