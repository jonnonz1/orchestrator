package agent

import (
	"bytes"
	"testing"
)

func TestWriteReadFrame(t *testing.T) {
	type testMsg struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	original := testMsg{Name: "test", Value: 42}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, &original); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	var decoded testMsg
	if err := ReadFrame(&buf, &decoded); err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}

	if decoded.Name != original.Name || decoded.Value != original.Value {
		t.Errorf("got %+v, want %+v", decoded, original)
	}
}

func TestWriteReadFrame_Request(t *testing.T) {
	req := Request{
		ID:   "test-123",
		Type: RequestTypePing,
	}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, &req); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	var decoded Request
	if err := ReadFrame(&buf, &decoded); err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}

	if decoded.ID != req.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, req.ID)
	}
	if decoded.Type != req.Type {
		t.Errorf("Type = %q, want %q", decoded.Type, req.Type)
	}
}

func TestWriteReadFrame_ExecRequest(t *testing.T) {
	req := Request{
		ID:   "exec-1",
		Type: RequestTypeExec,
		Exec: &ExecRequest{
			Command: []string{"echo", "hello"},
			Env:     map[string]string{"FOO": "bar"},
			WorkDir: "/tmp",
			Stream:  true,
		},
	}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, &req); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	var decoded Request
	if err := ReadFrame(&buf, &decoded); err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}

	if decoded.Exec == nil {
		t.Fatal("Exec is nil")
	}
	if decoded.Exec.Command[0] != "echo" {
		t.Errorf("Command[0] = %q, want echo", decoded.Exec.Command[0])
	}
	if decoded.Exec.Env["FOO"] != "bar" {
		t.Errorf("Env[FOO] = %q, want bar", decoded.Exec.Env["FOO"])
	}
	if !decoded.Exec.Stream {
		t.Error("Stream = false, want true")
	}
}

func TestReadFrame_TooLarge(t *testing.T) {
	// Write a frame header claiming 20MB
	var buf bytes.Buffer
	buf.Write([]byte{0x01, 0x40, 0x00, 0x00}) // 20971520 bytes

	var decoded Request
	err := ReadFrame(&buf, &decoded)
	if err == nil {
		t.Error("expected error for oversized frame")
	}
}

func TestWriteReadFrame_MultipleFrames(t *testing.T) {
	var buf bytes.Buffer

	for i := 0; i < 3; i++ {
		resp := Response{
			ID:   "test",
			Type: ResponseTypeOK,
		}
		if err := WriteFrame(&buf, &resp); err != nil {
			t.Fatalf("WriteFrame %d: %v", i, err)
		}
	}

	for i := 0; i < 3; i++ {
		var decoded Response
		if err := ReadFrame(&buf, &decoded); err != nil {
			t.Fatalf("ReadFrame %d: %v", i, err)
		}
		if decoded.Type != ResponseTypeOK {
			t.Errorf("frame %d: Type = %q, want ok", i, decoded.Type)
		}
	}
}
