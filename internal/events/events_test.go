package events

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type stubSink struct {
	mu   sync.Mutex
	evts []Event
}

func (s *stubSink) Emit(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evts = append(s.evts, ev)
}

func TestMultiFansOut(t *testing.T) {
	a, b := &stubSink{}, &stubSink{}
	m := Multi{a, nil, b}
	m.Emit(Event{Type: TypeTaskStarted})
	if len(a.evts) != 1 || len(b.evts) != 1 {
		t.Fatalf("want 1/1, got %d/%d", len(a.evts), len(b.evts))
	}
}

func TestAuditAppendsJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	al, err := NewAuditLogger(path, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()

	al.Emit(Event{ID: "e1", Type: TypeTaskStarted, Timestamp: time.Now(), TaskID: "t1"})
	al.Emit(Event{ID: "e2", Type: TypeTaskCompleted, Timestamp: time.Now(), TaskID: "t1"})

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var events []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		events = append(events, ev)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].ID != "e1" || events[1].ID != "e2" {
		t.Fatalf("unexpected order: %+v", events)
	}
}

func TestWebhookSigns(t *testing.T) {
	var (
		gotSig  string
		gotBody []byte
	)
	received := make(chan struct{}, 1)
	ts := newEchoServer(func(sig string, body []byte) {
		gotSig = sig
		gotBody = body
		received <- struct{}{}
	})
	defer ts.Close()

	s := NewWebhookSender(ts.URL, "shh", testLogger())
	if s == nil {
		t.Fatal("sender should not be nil")
	}
	s.Emit(Event{ID: "x", Type: TypeTaskStarted, Timestamp: time.Now()})

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("webhook never arrived")
	}
	if gotSig == "" {
		t.Error("no signature header")
	}
	if len(gotBody) == 0 {
		t.Error("no body")
	}
}

func TestNewWebhookSenderReturnsNilForEmptyURL(t *testing.T) {
	if s := NewWebhookSender("", "", testLogger()); s != nil {
		t.Fatal("expected nil for empty URL")
	}
}
