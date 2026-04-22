package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/jonnonz1/orchestrator/internal/agent"
)

const agentVersion = "0.1.0"

var startTime = time.Now()

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	log.Printf("guest agent %s starting", agentVersion)

	// Create AF_VSOCK socket
	listenFd, err := createVsockListener(agent.ControlPort)
	if err != nil {
		log.Fatalf("listen control port %d: %v", agent.ControlPort, err)
	}

	log.Printf("listening on vsock port %d (control)", agent.ControlPort)

	for {
		conn, err := acceptVsock(listenFd)
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	var req agent.Request
	if err := agent.ReadFrame(conn, &req); err != nil {
		log.Printf("read request: %v", err)
		return
	}

	log.Printf("request: id=%s type=%s", req.ID, req.Type)

	switch req.Type {
	case agent.RequestTypePing:
		handlePing(conn, &req)
	case agent.RequestTypeExec:
		handleExec(conn, &req)
	case agent.RequestTypeWriteFiles:
		handleWriteFiles(conn, &req)
	case agent.RequestTypeReadFile:
		handleReadFile(conn, &req)
	case agent.RequestTypeSignal:
		handleSignal(conn, &req)
	default:
		sendError(conn, req.ID, fmt.Sprintf("unknown request type: %s", req.Type))
	}
}

func handlePing(conn net.Conn, req *agent.Request) {
	resp := agent.Response{
		ID:   req.ID,
		Type: agent.ResponseTypeOK,
		AgentInfo: &agent.AgentInfo{
			Version: agentVersion,
			Uptime:  time.Since(startTime),
		},
	}
	agent.WriteFrame(conn, &resp)
}

func handleExec(conn net.Conn, req *agent.Request) {
	if req.Exec == nil || len(req.Exec.Command) == 0 {
		sendError(conn, req.ID, "exec: command is required")
		return
	}

	cmd := exec.Command(req.Exec.Command[0], req.Exec.Command[1:]...)

	// Set working directory
	if req.Exec.WorkDir != "" {
		cmd.Dir = req.Exec.WorkDir
	} else {
		cmd.Dir = "/root"
	}

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range req.Exec.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	if req.Exec.Stream {
		handleStreamingExec(conn, req, cmd)
	} else {
		handleBufferedExec(conn, req, cmd)
	}
}

func handleBufferedExec(conn net.Conn, req *agent.Request, cmd *exec.Cmd) {
	stdout, err := cmd.Output()
	exitCode := 0
	stderr := ""

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			stderr = string(exitErr.Stderr)
		} else {
			sendError(conn, req.ID, fmt.Sprintf("exec: %v", err))
			return
		}
	}

	resp := agent.Response{
		ID:   req.ID,
		Type: agent.ResponseTypeOK,
		ExecResult: &agent.ExecResult{
			ExitCode: exitCode,
			Stdout:   string(stdout),
			Stderr:   stderr,
		},
	}
	agent.WriteFrame(conn, &resp)
}

func handleStreamingExec(conn net.Conn, req *agent.Request, cmd *exec.Cmd) {
	// Run in its own process group so we can kill child processes
	// (e.g., background servers started by Claude Code)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		sendError(conn, req.ID, fmt.Sprintf("stdout pipe: %v", err))
		return
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		sendError(conn, req.ID, fmt.Sprintf("stderr pipe: %v", err))
		return
	}

	if err := cmd.Start(); err != nil {
		sendError(conn, req.ID, fmt.Sprintf("start: %v", err))
		return
	}

	// Send stream response to indicate streaming mode
	resp := agent.Response{
		ID:   req.ID,
		Type: agent.ResponseTypeStream,
	}
	if err := agent.WriteFrame(conn, &resp); err != nil {
		log.Printf("write stream response: %v", err)
		cmd.Process.Kill()
		return
	}

	// Stream stdout and stderr concurrently
	done := make(chan struct{}, 2)

	go streamPipe(conn, req.ID, agent.StreamEventStdout, stdoutPipe, done)
	go streamPipe(conn, req.ID, agent.StreamEventStderr, stderrPipe, done)

	// Wait for the main process to exit (NOT the pipes — they may stay
	// open if child processes inherited them, e.g., background servers)
	exitCode := 0
	waitDone := make(chan struct{})
	go func() {
		if err := cmd.Wait(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		close(waitDone)
	}()

	// Wait for process exit, then kill the entire process group to
	// close inherited pipes from background children
	<-waitDone

	// Kill the process group (negative PID = kill group)
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		syscall.Kill(-pgid, syscall.SIGTERM)
		// Give children a moment to exit, then force kill
		time.Sleep(500 * time.Millisecond)
		syscall.Kill(-pgid, syscall.SIGKILL)
	}

	// Now the pipes should close — wait for streamPipe goroutines
	// with a timeout in case they're still stuck
	pipeTimeout := time.After(3 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-pipeTimeout:
			log.Printf("pipe drain timeout, proceeding with exit event")
			i = 2 // break out
		}
	}

	// Send exit event
	exitEvent := agent.StreamEvent{
		ID:        req.ID,
		Type:      agent.StreamEventExit,
		Data:      strconv.Itoa(exitCode),
		Timestamp: time.Now(),
	}
	agent.WriteFrame(conn, &exitEvent)
}

func streamPipe(conn net.Conn, reqID string, eventType agent.StreamEventType, pipe io.ReadCloser, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()

	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 256*1024), 256*1024) // 256KB buffer for long lines

	for scanner.Scan() {
		event := agent.StreamEvent{
			ID:        reqID,
			Type:      eventType,
			Data:      scanner.Text(),
			Timestamp: time.Now(),
		}
		if err := agent.WriteFrame(conn, &event); err != nil {
			log.Printf("write stream event: %v", err)
			return
		}
	}
}

func handleWriteFiles(conn net.Conn, req *agent.Request) {
	if req.WriteFiles == nil {
		sendError(conn, req.ID, "write_files: files is required")
		return
	}

	for _, f := range req.WriteFiles.Files {
		if err := os.MkdirAll(filepath.Dir(f.Path), 0755); err != nil {
			sendError(conn, req.ID, fmt.Sprintf("mkdir %s: %v", filepath.Dir(f.Path), err))
			return
		}

		mode := os.FileMode(f.Mode)
		if mode == 0 {
			mode = 0644
		}

		if err := os.WriteFile(f.Path, f.Content, mode); err != nil {
			sendError(conn, req.ID, fmt.Sprintf("write %s: %v", f.Path, err))
			return
		}
		log.Printf("wrote file: %s (%d bytes)", f.Path, len(f.Content))
	}

	resp := agent.Response{ID: req.ID, Type: agent.ResponseTypeOK}
	agent.WriteFrame(conn, &resp)
}

func handleReadFile(conn net.Conn, req *agent.Request) {
	if req.ReadFile == nil {
		sendError(conn, req.ID, "read_file: path is required")
		return
	}

	data, err := os.ReadFile(req.ReadFile.Path)
	if err != nil {
		sendError(conn, req.ID, fmt.Sprintf("read %s: %v", req.ReadFile.Path, err))
		return
	}

	resp := agent.Response{
		ID:          req.ID,
		Type:        agent.ResponseTypeOK,
		FileContent: data,
	}
	agent.WriteFrame(conn, &resp)
}

func handleSignal(conn net.Conn, req *agent.Request) {
	if req.Signal == nil {
		sendError(conn, req.ID, "signal: pid and signal are required")
		return
	}

	proc, err := os.FindProcess(req.Signal.PID)
	if err != nil {
		sendError(conn, req.ID, fmt.Sprintf("find process %d: %v", req.Signal.PID, err))
		return
	}

	var sig syscall.Signal
	switch strings.ToUpper(req.Signal.Signal) {
	case "SIGTERM", "TERM":
		sig = syscall.SIGTERM
	case "SIGKILL", "KILL":
		sig = syscall.SIGKILL
	case "SIGINT", "INT":
		sig = syscall.SIGINT
	default:
		sendError(conn, req.ID, fmt.Sprintf("unknown signal: %s", req.Signal.Signal))
		return
	}

	if err := proc.Signal(sig); err != nil {
		sendError(conn, req.ID, fmt.Sprintf("signal %d: %v", req.Signal.PID, err))
		return
	}

	resp := agent.Response{ID: req.ID, Type: agent.ResponseTypeOK}
	agent.WriteFrame(conn, &resp)
}

func sendError(conn net.Conn, id, msg string) {
	log.Printf("error: %s", msg)
	resp := agent.Response{
		ID:    id,
		Type:  agent.ResponseTypeError,
		Error: msg,
	}
	agent.WriteFrame(conn, &resp)
}

// createVsockListener creates an AF_VSOCK socket, binds and listens.
func createVsockListener(port int) (int, error) {
	fd, err := syscall.Socket(40, syscall.SOCK_STREAM, 0) // 40 = AF_VSOCK
	if err != nil {
		return -1, fmt.Errorf("socket: %w", err)
	}

	// struct sockaddr_vm layout (16 bytes):
	//   u16 family    (offset 0)
	//   u16 reserved  (offset 2)
	//   u32 port      (offset 4)
	//   u32 cid       (offset 8)
	//   u8  zero[4]   (offset 12)
	sa := [16]byte{}
	*(*uint16)(unsafe.Pointer(&sa[0])) = 40          // AF_VSOCK
	*(*uint32)(unsafe.Pointer(&sa[4])) = uint32(port) // port
	*(*uint32)(unsafe.Pointer(&sa[8])) = 0xFFFFFFFF   // VMADDR_CID_ANY

	_, _, errno := syscall.RawSyscall(syscall.SYS_BIND, uintptr(fd), uintptr(unsafe.Pointer(&sa[0])), 16)
	if errno != 0 {
		syscall.Close(fd)
		return -1, fmt.Errorf("bind: %v", errno)
	}

	_, _, errno = syscall.RawSyscall(syscall.SYS_LISTEN, uintptr(fd), 5, 0)
	if errno != 0 {
		syscall.Close(fd)
		return -1, fmt.Errorf("listen: %v", errno)
	}

	return fd, nil
}

// acceptVsock accepts a connection on the vsock fd and wraps it as net.Conn.
func acceptVsock(listenFd int) (net.Conn, error) {
	// Use raw syscall for accept4 since Go's syscall.Accept doesn't handle AF_VSOCK
	nfd, _, errno := syscall.Syscall6(
		syscall.SYS_ACCEPT4,
		uintptr(listenFd),
		0, // addr (NULL - we don't need peer address)
		0, // addrlen (NULL)
		0, // flags
		0, 0,
	)
	if errno != 0 {
		return nil, fmt.Errorf("accept4: %v", errno)
	}

	return &vsockConn{fd: int(nfd)}, nil
}

// vsockConn wraps a raw fd as a net.Conn for AF_VSOCK.
type vsockConn struct {
	fd int
}

func (c *vsockConn) Read(b []byte) (int, error) {
	n, err := syscall.Read(c.fd, b)
	if n == 0 && err == nil {
		return 0, io.EOF
	}
	return n, err
}

func (c *vsockConn) Write(b []byte) (int, error) {
	return syscall.Write(c.fd, b)
}

func (c *vsockConn) Close() error {
	return syscall.Close(c.fd)
}

func (c *vsockConn) LocalAddr() net.Addr                { return vsockAddr{} }
func (c *vsockConn) RemoteAddr() net.Addr               { return vsockAddr{} }
func (c *vsockConn) SetDeadline(t time.Time) error      { return nil }
func (c *vsockConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *vsockConn) SetWriteDeadline(t time.Time) error { return nil }

type vsockAddr struct{}

func (vsockAddr) Network() string { return "vsock" }
func (vsockAddr) String() string  { return "vsock" }
