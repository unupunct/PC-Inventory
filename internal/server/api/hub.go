package api

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Hub is a tiny SSE broadcaster.
type Hub struct {
	mu      sync.RWMutex
	clients map[chan sseEvent]struct{}
}

type sseEvent struct {
	Event string
	Data  any
}

func NewHub() *Hub { return &Hub{clients: map[chan sseEvent]struct{}{}} }

func (h *Hub) Broadcast(event string, payload any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- sseEvent{Event: event, Data: payload}:
		default: // slow client: drop the event rather than block reporters
		}
	}
}

func (h *Hub) subscribe() chan sseEvent {
	ch := make(chan sseEvent, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) unsubscribe(ch chan sseEvent) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)

	ch := s.Hub.subscribe()
	defer s.Hub.unsubscribe(ch)

	fmt.Fprintf(w, "event: hello\ndata: {\"time\":%q}\n\n", time.Now().UTC().Format(time.RFC3339))
	flusher.Flush()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data := "{}"
			if ev.Data != nil {
				if b, err := jsonMarshal(ev.Data); err == nil {
					data = string(b)
				}
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Event, data)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive %d\n\n", time.Now().Unix())
			flusher.Flush()
		}
	}
}
