// Package relay matches CLI exec requests to sandbox agents over long-poll HTTP.
package relay

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"crypto/rand"
	"encoding/hex"
)

// Hub matches CLI exec requests to sandbox agents (long-poll).
type Hub struct {
	mu      sync.Mutex
	pending map[string]chan job // sandbox -> next job waiter (agent poll)
	waiters map[string]chan result
}

type job struct {
	ID   string   `json:"id"`
	Argv []string `json:"argv"`
}

type result struct {
	ID       string `json:"id"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

// NewHub creates an empty relay hub.
func NewHub() *Hub {
	return &Hub{
		pending: map[string]chan job{},
		waiters: map[string]chan result{},
	}
}

// Mount registers /v1/relay/ routes on mux.
func (h *Hub) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/relay/", h.serve)
}

func (h *Hub) serve(w http.ResponseWriter, r *http.Request) {
	// /v1/relay/{name}/poll|exec|result
	path := r.URL.Path[len("/v1/relay/"):]
	parts := split2(path)
	if len(parts) != 2 {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	name, action := parts[0], parts[1]
	switch action {
	case "poll":
		h.poll(w, r, name)
	case "exec":
		h.exec(w, r, name)
	case "result":
		h.result(w, r, name)
	default:
		http.Error(w, "unknown action", http.StatusNotFound)
	}
}

func (h *Hub) poll(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	h.mu.Lock()
	ch := make(chan job, 1)
	h.pending[name] = ch
	h.mu.Unlock()

	select {
	case j := <-ch:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(j)
	case <-time.After(55 * time.Second):
		w.WriteHeader(http.StatusNoContent)
	case <-r.Context().Done():
		w.WriteHeader(http.StatusNoContent)
	}
	h.mu.Lock()
	if h.pending[name] == ch {
		delete(h.pending, name)
	}
	h.mu.Unlock()
}

func (h *Hub) exec(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Argv []string `json:"argv"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Argv) == 0 {
		http.Error(w, "argv required", http.StatusBadRequest)
		return
	}
	id := newID()
	resCh := make(chan result, 1)
	h.mu.Lock()
	h.waiters[id] = resCh
	agentCh := h.pending[name]
	h.mu.Unlock()
	if agentCh == nil {
		h.mu.Lock()
		delete(h.waiters, id)
		h.mu.Unlock()
		http.Error(w, "agent not connected", http.StatusServiceUnavailable)
		return
	}
	select {
	case agentCh <- job{ID: id, Argv: req.Argv}:
	default:
		h.mu.Lock()
		delete(h.waiters, id)
		h.mu.Unlock()
		http.Error(w, "agent busy", http.StatusConflict)
		return
	}
	select {
	case res := <-resCh:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	case <-time.After(60 * time.Second):
		http.Error(w, "timeout", http.StatusGatewayTimeout)
	}
	h.mu.Lock()
	delete(h.waiters, id)
	h.mu.Unlock()
}

func (h *Hub) result(w http.ResponseWriter, r *http.Request, name string) {
	_ = name
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var res result
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	ch := h.waiters[res.ID]
	h.mu.Unlock()
	if ch == nil {
		http.Error(w, "unknown job", http.StatusNotFound)
		return
	}
	ch <- res
	w.WriteHeader(http.StatusNoContent)
}

func split2(path string) []string {
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			return []string{path[:i], path[i+1:]}
		}
	}
	return nil
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
