package events

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
)

// newEchoServer returns an httptest server that captures the signature
// header and body for inspection in tests.
func newEchoServer(onPost func(sig string, body []byte)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		onPost(r.Header.Get("X-Orchestrator-Signature"), body)
		w.WriteHeader(http.StatusOK)
	}))
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
