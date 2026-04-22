package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLoopback(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:8080", true},
		{"[::1]:8080", true},
		{"0.0.0.0:8080", false},
		{"192.168.1.10:8080", false},
		{":8080", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsLoopback(c.addr); got != c.want {
			t.Errorf("IsLoopback(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}

func TestMiddlewareNoToken(t *testing.T) {
	h := Middleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/any", nil))
	if w.Code != http.StatusTeapot {
		t.Fatalf("no-token middleware should pass through, got %d", w.Code)
	}
}

func TestMiddlewareRejects(t *testing.T) {
	h := Middleware("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/any", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing token should be 401, got %d", w.Code)
	}
}

func TestMiddlewareAccepts(t *testing.T) {
	h := Middleware("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/any", nil)
	r.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTeapot {
		t.Fatalf("valid token should pass, got %d", w.Code)
	}
}

func TestMiddlewareHealthBypass(t *testing.T) {
	h := Middleware("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("health endpoint should bypass auth, got %d", w.Code)
	}
}

func TestGenerateToken(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("tokens should not collide")
	}
	if len(a) != 64 {
		t.Fatalf("token length = %d, want 64 hex chars", len(a))
	}
}
