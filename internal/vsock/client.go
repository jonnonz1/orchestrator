package vsock

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/jonnonz1/orchestrator/internal/agent"
	"github.com/jonnonz1/orchestrator/internal/config"
)

// Connect establishes a vsock connection to a guest agent port.
// Firecracker exposes vsock as a UDS — we connect to the socket file
// and send a CONNECT command to reach the guest port.
func Connect(jailID string, port int) (net.Conn, error) {
	socketPath := filepath.Join(config.Get().JailerBase, jailID, "root", "vsock.sock")

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial vsock UDS %s: %w", socketPath, err)
	}

	// Send CONNECT command
	connectCmd := fmt.Sprintf("CONNECT %d\n", port)
	if _, err := conn.Write([]byte(connectCmd)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("vsock CONNECT: %w", err)
	}

	// Read response
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("vsock response: %w", err)
	}

	resp := strings.TrimSpace(string(buf[:n]))
	if !strings.HasPrefix(resp, "OK") {
		conn.Close()
		return nil, fmt.Errorf("vsock rejected: %s", resp)
	}

	return conn, nil
}

// Ping sends a ping request to the guest agent and returns the response.
func Ping(jailID string) (*agent.AgentInfo, error) {
	conn, err := Connect(jailID, agent.ControlPort)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := agent.Request{
		ID:   "ping",
		Type: agent.RequestTypePing,
	}

	if err := agent.WriteFrame(conn, &req); err != nil {
		return nil, fmt.Errorf("write ping: %w", err)
	}

	var resp agent.Response
	if err := agent.ReadFrame(conn, &resp); err != nil {
		return nil, fmt.Errorf("read ping response: %w", err)
	}

	if resp.Type == agent.ResponseTypeError {
		return nil, fmt.Errorf("ping error: %s", resp.Error)
	}

	return resp.AgentInfo, nil
}

// Exec runs a command on the guest and returns the result (non-streaming).
func Exec(jailID string, command []string, env map[string]string, workDir string) (*agent.ExecResult, error) {
	conn, err := Connect(jailID, agent.ControlPort)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := agent.Request{
		ID:   fmt.Sprintf("exec-%d", time.Now().UnixNano()),
		Type: agent.RequestTypeExec,
		Exec: &agent.ExecRequest{
			Command: command,
			Env:     env,
			WorkDir: workDir,
			Stream:  false,
		},
	}

	if err := agent.WriteFrame(conn, &req); err != nil {
		return nil, fmt.Errorf("write exec: %w", err)
	}

	var resp agent.Response
	if err := agent.ReadFrame(conn, &resp); err != nil {
		return nil, fmt.Errorf("read exec response: %w", err)
	}

	if resp.Type == agent.ResponseTypeError {
		return nil, fmt.Errorf("exec error: %s", resp.Error)
	}

	return resp.ExecResult, nil
}

// ExecStream runs a command and streams output back via a callback.
func ExecStream(jailID string, command []string, env map[string]string, workDir string, onEvent func(agent.StreamEvent)) (*agent.ExecResult, error) {
	conn, err := Connect(jailID, agent.ControlPort)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := agent.Request{
		ID:   fmt.Sprintf("exec-%d", time.Now().UnixNano()),
		Type: agent.RequestTypeExec,
		Exec: &agent.ExecRequest{
			Command: command,
			Env:     env,
			WorkDir: workDir,
			Stream:  true,
		},
	}

	if err := agent.WriteFrame(conn, &req); err != nil {
		return nil, fmt.Errorf("write exec: %w", err)
	}

	// Read initial response
	var resp agent.Response
	if err := agent.ReadFrame(conn, &resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.Type == agent.ResponseTypeError {
		return nil, fmt.Errorf("exec error: %s", resp.Error)
	}

	if resp.Type != agent.ResponseTypeStream {
		return resp.ExecResult, nil
	}

	// Read stream events
	for {
		var event agent.StreamEvent
		if err := agent.ReadFrame(conn, &event); err != nil {
			return nil, fmt.Errorf("read stream event: %w", err)
		}

		if onEvent != nil {
			onEvent(event)
		}

		if event.Type == agent.StreamEventExit {
			exitCode := 0
			fmt.Sscanf(event.Data, "%d", &exitCode)
			return &agent.ExecResult{ExitCode: exitCode}, nil
		}
	}
}

// WriteFiles sends files to be written on the guest.
func WriteFiles(jailID string, files []agent.FileEntry) error {
	conn, err := Connect(jailID, agent.ControlPort)
	if err != nil {
		return err
	}
	defer conn.Close()

	req := agent.Request{
		ID:   fmt.Sprintf("write-%d", time.Now().UnixNano()),
		Type: agent.RequestTypeWriteFiles,
		WriteFiles: &agent.WriteFilesRequest{
			Files: files,
		},
	}

	if err := agent.WriteFrame(conn, &req); err != nil {
		return fmt.Errorf("write request: %w", err)
	}

	var resp agent.Response
	if err := agent.ReadFrame(conn, &resp); err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.Type == agent.ResponseTypeError {
		return fmt.Errorf("write_files error: %s", resp.Error)
	}

	return nil
}

// ReadFile reads a file from the guest.
func ReadFile(jailID string, path string) ([]byte, error) {
	conn, err := Connect(jailID, agent.ControlPort)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := agent.Request{
		ID:   fmt.Sprintf("read-%d", time.Now().UnixNano()),
		Type: agent.RequestTypeReadFile,
		ReadFile: &agent.ReadFileRequest{
			Path: path,
		},
	}

	if err := agent.WriteFrame(conn, &req); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	var resp agent.Response
	if err := agent.ReadFrame(conn, &resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.Type == agent.ResponseTypeError {
		return nil, fmt.Errorf("read_file error: %s", resp.Error)
	}

	return resp.FileContent, nil
}
