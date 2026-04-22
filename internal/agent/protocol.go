package agent

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Ports used by the guest agent.
const (
	ControlPort = 9001
	LogPort     = 9002
)

// RequestType identifies the type of request from host to guest.
type RequestType string

const (
	RequestTypeExec       RequestType = "exec"
	RequestTypeWriteFiles RequestType = "write_files"
	RequestTypeReadFile   RequestType = "read_file"
	RequestTypeSignal     RequestType = "signal"
	RequestTypePing       RequestType = "ping"
)

// Request is a message from host to guest over vsock.
type Request struct {
	ID         string            `json:"id"`
	Type       RequestType       `json:"type"`
	Exec       *ExecRequest      `json:"exec,omitempty"`
	WriteFiles *WriteFilesRequest `json:"write_files,omitempty"`
	ReadFile   *ReadFileRequest  `json:"read_file,omitempty"`
	Signal     *SignalRequest    `json:"signal,omitempty"`
}

// ExecRequest asks the agent to run a command.
type ExecRequest struct {
	Command []string          `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
	WorkDir string            `json:"work_dir,omitempty"`
	Stream  bool              `json:"stream"`
}

// WriteFilesRequest asks the agent to write files to disk.
type WriteFilesRequest struct {
	Files []FileEntry `json:"files"`
}

// FileEntry represents a single file to write.
type FileEntry struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
	Mode    uint32 `json:"mode"`
}

// ReadFileRequest asks the agent to read a file.
type ReadFileRequest struct {
	Path string `json:"path"`
}

// SignalRequest asks the agent to send a signal to a process.
type SignalRequest struct {
	PID    int    `json:"pid"`
	Signal string `json:"signal"`
}

// ResponseType identifies the type of response from guest to host.
type ResponseType string

const (
	ResponseTypeOK     ResponseType = "ok"
	ResponseTypeError  ResponseType = "error"
	ResponseTypeStream ResponseType = "stream"
)

// Response is a message from guest to host over vsock.
type Response struct {
	ID         string       `json:"id"`
	Type       ResponseType `json:"type"`
	Error      string       `json:"error,omitempty"`
	ExecResult *ExecResult  `json:"exec_result,omitempty"`
	FileContent []byte      `json:"file_content,omitempty"`
	AgentInfo  *AgentInfo   `json:"agent_info,omitempty"`
}

// ExecResult is the result of a non-streaming exec.
type ExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

// AgentInfo is returned by ping requests.
type AgentInfo struct {
	Version string        `json:"version"`
	Uptime  time.Duration `json:"uptime"`
}

// StreamEventType identifies the type of streaming event.
type StreamEventType string

const (
	StreamEventStdout StreamEventType = "stdout"
	StreamEventStderr StreamEventType = "stderr"
	StreamEventExit   StreamEventType = "exit"
)

// StreamEvent is sent during a streaming exec.
type StreamEvent struct {
	ID        string          `json:"id"`
	Type      StreamEventType `json:"type"`
	Data      string          `json:"data,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// WriteFrame writes a length-prefixed JSON frame to a writer.
func WriteFrame(w io.Writer, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Write 4-byte big-endian length prefix
	length := uint32(len(data))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return fmt.Errorf("write length: %w", err)
	}

	// Write payload
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}

	return nil
}

// ReadFrame reads a length-prefixed JSON frame from a reader.
func ReadFrame(r io.Reader, v interface{}) error {
	// Read 4-byte big-endian length prefix
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return fmt.Errorf("read length: %w", err)
	}

	if length > 10*1024*1024 { // 10MB max
		return fmt.Errorf("frame too large: %d bytes", length)
	}

	// Read payload
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return fmt.Errorf("read payload: %w", err)
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	return nil
}
