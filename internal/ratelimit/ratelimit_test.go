package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSemaphoreBlocksAtCap(t *testing.T) {
	l := New(2, 0)

	rel1, err := l.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rel2, err := l.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = l.Acquire(ctx)
	if err == nil {
		t.Fatal("expected error at cap=2 with 2 held")
	}

	rel1()
	rel3, err := l.Acquire(context.Background())
	if err != nil {
		t.Fatal("should acquire after release:", err)
	}
	rel2()
	rel3()
}

func TestRateLimitDeniesExcess(t *testing.T) {
	l := New(0, 3)
	for i := 0; i < 3; i++ {
		if err := l.TryRate(); err != nil {
			t.Fatalf("token %d should succeed: %v", i, err)
		}
	}
	if err := l.TryRate(); err == nil {
		t.Fatal("4th token should fail with rate=3/min")
	}
}

func TestMiddleware429(t *testing.T) {
	l := New(0, 1)
	h := l.HTTPMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tasks", nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("first POST should pass, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tasks", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second POST should 429, got %d", w.Code)
	}
}

func TestNilLimiterPassesThrough(t *testing.T) {
	var l *Limiter
	rel, err := l.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rel()
	if err := l.TryRate(); err != nil {
		t.Fatal(err)
	}
}
