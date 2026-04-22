package events

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// WebhookSender delivers events to a URL over HTTP POST with an HMAC
// signature in the X-Orchestrator-Signature header (hex-encoded SHA-256).
//
// Delivery is best-effort and non-blocking — events are sent in a goroutine
// so a slow receiver cannot stall the task runner. Failures are logged.
type WebhookSender struct {
	URL    string
	Secret string
	Client *http.Client
	Log    *slog.Logger
}

// NewWebhookSender returns a sender or nil if URL is empty. Only http and
// https schemes are accepted — anything else (file://, gopher://, or just a
// malformed URL) is rejected at construction time with a warning.
func NewWebhookSender(rawURL, secret string, log *slog.Logger) *WebhookSender {
	if rawURL == "" {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		if log != nil {
			log.Warn("webhook URL rejected — only absolute http(s) URLs are accepted", "url", rawURL)
		}
		return nil
	}
	return &WebhookSender{
		URL:    rawURL,
		Secret: secret,
		Client: &http.Client{Timeout: 5 * time.Second},
		Log:    log,
	}
}

// Emit implements Sink.
func (s *WebhookSender) Emit(ev Event) {
	if s == nil {
		return
	}
	go s.deliver(ev)
}

func (s *WebhookSender) deliver(ev Event) {
	body, err := json.Marshal(ev)
	if err != nil {
		s.Log.Warn("webhook marshal", "error", err)
		return
	}
	req, err := http.NewRequest("POST", s.URL, bytes.NewReader(body))
	if err != nil {
		s.Log.Warn("webhook request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "orchestrator-webhook/1.0")
	if s.Secret != "" {
		mac := hmac.New(sha256.New, []byte(s.Secret))
		mac.Write(body)
		req.Header.Set("X-Orchestrator-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		s.Log.Warn("webhook send", "type", ev.Type, "error", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		s.Log.Warn("webhook non-2xx", "type", ev.Type, "status", resp.StatusCode)
	}
}
