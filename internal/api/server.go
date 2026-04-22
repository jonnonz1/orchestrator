package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jonnonz1/orchestrator/internal/authn"
	"github.com/jonnonz1/orchestrator/internal/metrics"
	"github.com/jonnonz1/orchestrator/internal/ratelimit"
	"github.com/jonnonz1/orchestrator/internal/stream"
	"github.com/jonnonz1/orchestrator/internal/task"
	"github.com/jonnonz1/orchestrator/internal/vm"
)

// WebDist is set from the main package via go:embed.
var WebDist fs.FS

// Server is the HTTP API server.
type Server struct {
	vmMgr       *vm.Manager
	taskStore   *task.Store
	taskRunner  *task.Runner
	streamHub   *stream.Hub
	log         *slog.Logger
	router      chi.Router
	authToken   string
	corsOrigins []string
	metrics     *metrics.Collector
	limiter     *ratelimit.Limiter
}

// NewServer creates a new API server.
func NewServer(vmMgr *vm.Manager, taskStore *task.Store, taskRunner *task.Runner, streamHub *stream.Hub, log *slog.Logger) *Server {
	s := &Server{
		vmMgr:      vmMgr,
		taskStore:  taskStore,
		taskRunner: taskRunner,
		streamHub:  streamHub,
		log:        log,
	}
	s.limiter = ratelimit.FromEnv()
	s.metrics = metrics.New(
		func() int {
			n := 0
			for _, v := range vmMgr.List() {
				if v.State == vm.VMStateRunning {
					n++
				}
			}
			return n
		},
		func() int {
			n := 0
			for _, t := range taskStore.List() {
				if t.Status == task.StatusRunning {
					n++
				}
			}
			return n
		},
	)
	taskRunner.Metrics = s.metrics
	s.router = s.buildRouter()
	return s
}

// Metrics returns the metrics collector so callers can expose it on a separate listener.
func (s *Server) Metrics() *metrics.Collector { return s.metrics }

// SetAuthToken enables bearer-token auth on API routes. Empty disables.
func (s *Server) SetAuthToken(token string) {
	s.authToken = token
	s.router = s.buildRouter()
}

// AuthToken returns the configured bearer token (empty if auth is disabled).
func (s *Server) AuthToken() string { return s.authToken }

// SetCORSOrigins configures the list of allowed CORS origins. Empty disables
// CORS entirely (same-origin only). Use ["*"] with caution — credentials are
// never forwarded when the wildcard is used.
func (s *Server) SetCORSOrigins(origins []string) {
	s.corsOrigins = origins
	s.router = s.buildRouter()
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// CORS: only install the middleware when the operator has explicitly
	// listed origins. Default = no cross-origin headers (same-origin only),
	// which is the safer default for a server that runs as root and can
	// execute arbitrary code in VMs. The legacy `AllowedOrigins: ["*"] +
	// AllowCredentials: true` combination was a footgun: any site a user
	// visits could issue credentialed requests to a loopback orchestrator.
	if len(s.corsOrigins) > 0 {
		wildcard := false
		for _, o := range s.corsOrigins {
			if o == "*" {
				wildcard = true
				break
			}
		}
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   s.corsOrigins,
			AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
			ExposedHeaders:   []string{"WWW-Authenticate"},
			AllowCredentials: !wildcard, // wildcard + credentials is browser-rejected anyway
			MaxAge:           300,
		}))
	}

	// Bearer-token auth (no-op if token empty)
	r.Use(authn.Middleware(s.authToken))
	// Rate limit on task/VM creation (no-op if unconfigured)
	r.Use(s.limiter.HTTPMiddleware())

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})

		// Prometheus-format metrics
		r.Get("/metrics", s.metrics.Handler())

		// Stats
		r.Get("/stats", s.handleStats)

		// VMs
		r.Route("/vms", func(r chi.Router) {
			r.Post("/", s.handleCreateVM)
			r.Get("/", s.handleListVMs)
			r.Get("/{name}", s.handleGetVM)
			r.Delete("/{name}", s.handleDeleteVM)
			r.Post("/{name}/stop", s.handleStopVM)
			r.Post("/{name}/exec", s.handleExecVM)
		})

		// Tasks
		r.Route("/tasks", func(r chi.Router) {
			r.Post("/", s.handleCreateTask)
			r.Get("/", s.handleListTasks)
			r.Get("/{id}", s.handleGetTask)
			r.Delete("/{id}", s.handleCancelTask)
			r.Get("/{id}/stream", s.handleTaskStream)
			r.Get("/{id}/files", s.handleListTaskFiles)
			r.Get("/{id}/files/{filename}", s.handleGetTaskFile)
		})
	})

	// Serve embedded frontend (SPA fallback). Requests to /api/* that reach
	// this handler never matched an API route, so respond with 404 instead of
	// silently returning index.html (which would mask client bugs).
	if WebDist != nil {
		subFS := WebDist
		fileServer := http.FileServer(http.FS(subFS))
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" {
				path = "index.html"
			}
			f, err := subFS.Open(path)
			if err != nil {
				// Serve index.html for SPA routing
				indexData, _ := fs.ReadFile(subFS, "index.html")
				w.Header().Set("Content-Type", "text/html")
				w.Write(indexData)
				return
			}
			f.Close()
			fileServer.ServeHTTP(w, r)
		})
	}

	return r
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	vms := s.vmMgr.List()
	running := 0
	for _, v := range vms {
		if v.State == vm.VMStateRunning {
			running++
		}
	}

	writeJSON(w, http.StatusOK, StatsResponse{
		TotalVMs:   len(vms),
		RunningVMs: running,
		TotalTasks: len(s.taskStore.List()),
	})
}

// ListenAndServe starts the HTTP server with conservative timeouts to
// mitigate slowloris and similar denial-of-service attacks on exposed
// deployments. WebSocket streaming ignores WriteTimeout because the
// underlying net.Conn is hijacked — long-lived streams are unaffected.
func (s *Server) ListenAndServe(addr string) error {
	s.log.Info("API server starting", "addr", addr)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0, // 0 = no deadline (needed for WebSocket + streaming downloads)
		IdleTimeout:       120 * time.Second,
	}
	return srv.ListenAndServe()
}

// Handler returns the http.Handler for embedding.
func (s *Server) Handler() http.Handler {
	return s.router
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}

// FormatAddr returns the full address string.
func FormatAddr(port int) string {
	return fmt.Sprintf(":%d", port)
}
