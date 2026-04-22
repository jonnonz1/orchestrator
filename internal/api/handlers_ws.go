package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/coder/websocket"
)

// WSMessage is sent over WebSocket connections.
type WSMessage struct {
	Type      string    `json:"type"`
	Data      string    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

func (s *Server) handleTaskStream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Verify task exists
	_, err := s.taskStore.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Origin handling: by default nhooyr/websocket enforces same-origin, which
	// is the right behaviour for a root-privileged server. When the operator
	// has explicitly allowlisted CORS origins, extend the same list to the
	// WebSocket upgrade so the dashboard on those origins can connect.
	opts := &websocket.AcceptOptions{}
	if len(s.corsOrigins) > 0 {
		opts.OriginPatterns = s.corsOrigins
	}

	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		s.log.Error("websocket accept", "error", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	ctx := r.Context()

	stream := s.streamHub.GetOrCreate(id)
	history, ch := stream.Subscribe()
	defer stream.Unsubscribe(ch)

	// Send buffered history
	for _, event := range history {
		msg := WSMessage{
			Type:      string(event.Type),
			Data:      event.Data,
			Timestamp: event.Timestamp,
		}
		data, _ := json.Marshal(msg)
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			return
		}
	}

	// Stream new events
	for event := range ch {
		msg := WSMessage{
			Type:      string(event.Type),
			Data:      event.Data,
			Timestamp: event.Timestamp,
		}
		data, _ := json.Marshal(msg)
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			return
		}

		// Close after exit event
		if event.Type == "exit" {
			return
		}
	}
}
