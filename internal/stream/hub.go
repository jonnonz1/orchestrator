package stream

import (
	"sync"

	"github.com/jonnonz1/orchestrator/internal/agent"
)

// Hub manages per-task stream fan-out to multiple subscribers.
type Hub struct {
	mu      sync.RWMutex
	streams map[string]*Stream
}

// NewHub creates a new stream hub.
func NewHub() *Hub {
	return &Hub{
		streams: make(map[string]*Stream),
	}
}

// GetOrCreate returns an existing stream or creates a new one.
func (h *Hub) GetOrCreate(id string) *Stream {
	h.mu.Lock()
	defer h.mu.Unlock()

	if s, ok := h.streams[id]; ok {
		return s
	}

	s := NewStream(1000) // 1000 line ring buffer
	h.streams[id] = s
	return s
}

// Get returns a stream if it exists.
func (h *Hub) Get(id string) *Stream {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.streams[id]
}

// Remove deletes a stream.
func (h *Hub) Remove(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if s, ok := h.streams[id]; ok {
		s.Close()
		delete(h.streams, id)
	}
}

// Stream is a fan-out channel with a ring buffer for backfill.
type Stream struct {
	mu          sync.RWMutex
	buffer      []agent.StreamEvent
	bufferSize  int
	bufferIdx   int
	bufferFull  bool
	subscribers map[chan agent.StreamEvent]struct{}
	closed      bool
	dropped     uint64 // events dropped because a subscriber was full
}

// NewStream creates a new stream with the given buffer size.
func NewStream(bufferSize int) *Stream {
	return &Stream{
		buffer:      make([]agent.StreamEvent, bufferSize),
		bufferSize:  bufferSize,
		subscribers: make(map[chan agent.StreamEvent]struct{}),
	}
}

// Publish sends an event to all subscribers and adds it to the ring buffer.
// If a subscriber's 64-entry queue is full the event is dropped for that
// subscriber (fresh events are preferred over stalled delivery); Dropped()
// counts such drops so operators can detect a stuck dashboard.
func (s *Stream) Publish(event agent.StreamEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	// Add to ring buffer
	s.buffer[s.bufferIdx] = event
	s.bufferIdx = (s.bufferIdx + 1) % s.bufferSize
	if s.bufferIdx == 0 {
		s.bufferFull = true
	}

	// Fan out to subscribers (non-blocking)
	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
			// Subscriber is slow; drop for that subscriber and account for it.
			s.dropped++
		}
	}
}

// Dropped returns the cumulative count of events dropped because a subscriber
// was too slow. Useful to expose as a Prometheus counter for alerting.
func (s *Stream) Dropped() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dropped
}

// Subscribe returns a channel that receives events, plus buffered history.
func (s *Stream) Subscribe() (history []agent.StreamEvent, ch chan agent.StreamEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Collect buffered history
	if s.bufferFull {
		history = make([]agent.StreamEvent, s.bufferSize)
		copy(history, s.buffer[s.bufferIdx:])
		copy(history[s.bufferSize-s.bufferIdx:], s.buffer[:s.bufferIdx])
	} else {
		history = make([]agent.StreamEvent, s.bufferIdx)
		copy(history, s.buffer[:s.bufferIdx])
	}

	ch = make(chan agent.StreamEvent, 64)
	s.subscribers[ch] = struct{}{}
	return history, ch
}

// Unsubscribe removes a subscriber channel.
func (s *Stream) Unsubscribe(ch chan agent.StreamEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subscribers, ch)
	close(ch)
}

// Close marks the stream as closed and closes all subscriber channels.
func (s *Stream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	for ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, ch)
	}
}
