// Package authn provides bearer-token authentication middleware and helpers.
//
// The orchestrator's API/MCP/dashboard endpoints run with root-equivalent
// authority (they can spawn VMs, execute arbitrary code in those VMs, and
// inject host-side credentials). Accordingly:
//
//   - By default the servers bind to 127.0.0.1 (loopback only).
//   - When the operator binds a non-loopback address, an auth token is
//     required. If none is provided, one is generated on startup and
//     printed to stderr.
//   - Requests to loopback-bound servers bypass auth; this preserves the
//     low-friction local dev experience while preventing the common
//     misconfiguration of exposing the orchestrator unauthenticated on a LAN.
package authn

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// GenerateToken returns a cryptographically-random 32-byte hex-encoded token.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// IsLoopback returns true if the given bind address is loopback-only.
// Accepts "127.0.0.1:8080", "[::1]:8080", "localhost:8080", ":8080"
// (any-interface — returns false), etc.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Try parsing as host-only
		host = addr
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// ResolveToken returns (token, generated). If provided is non-empty it is
// returned unchanged and generated=false. Otherwise a fresh token is
// generated (generated=true).
func ResolveToken(provided string) (string, bool, error) {
	if provided != "" {
		return provided, false, nil
	}
	tok, err := GenerateToken()
	if err != nil {
		return "", false, err
	}
	return tok, true, nil
}

// PolicyFor selects the auth policy given a bind address and a provided token.
//
// Returns:
//   - token: the token that clients must present (empty if auth disabled)
//   - enabled: whether auth middleware should be enforced
//   - err: non-nil if binding is unsafe and auth cannot be established
func PolicyFor(addr, providedToken string, log *slog.Logger) (token string, enabled bool, err error) {
	if IsLoopback(addr) {
		if providedToken != "" {
			log.Info("auth enabled (loopback + explicit token)")
			return providedToken, true, nil
		}
		log.Info("auth disabled (loopback-only bind)")
		return "", false, nil
	}

	tok, generated, err := ResolveToken(providedToken)
	if err != nil {
		return "", false, err
	}
	if generated {
		// Print to stderr so operators see it without log-level filtering.
		fmt.Fprintf(authStderr, "\n===================================================================\n")
		fmt.Fprintf(authStderr, "  ORCHESTRATOR AUTH TOKEN (keep this secret):\n\n")
		fmt.Fprintf(authStderr, "      %s\n\n", tok)
		fmt.Fprintf(authStderr, "  Clients must send 'Authorization: Bearer <token>' on every request.\n")
		fmt.Fprintf(authStderr, "  Set ORCHESTRATOR_AUTH_TOKEN or pass --auth-token to fix the token across restarts.\n")
		fmt.Fprintf(authStderr, "===================================================================\n\n")
	}
	log.Info("auth enabled (non-loopback bind)", "generated", generated)
	return tok, true, nil
}

// Middleware returns an HTTP middleware that requires a bearer token match.
// If token is empty, middleware is a no-op. The token may be supplied as
// either `Authorization: Bearer <token>` (preferred) or `?token=<token>`
// (needed for WebSocket upgrades, which browsers don't allow custom headers on).
// Liveness endpoints (/api/v1/health, /healthz) and the Prometheus scrape
// endpoint (/api/v1/metrics) always pass — operators can gate those at the
// network layer if needed.
func Middleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}
		want := []byte(token)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/health", "/healthz", "/api/v1/metrics":
				next.ServeHTTP(w, r)
				return
			}
			got := tokenFromRequest(r)
			if got == "" || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
				unauthorized(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// tokenFromRequest extracts a bearer token from the Authorization header or
// ?token= query string. Returns empty string if neither is present.
func tokenFromRequest(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return r.URL.Query().Get("token")
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="orchestrator"`)
	http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
}
