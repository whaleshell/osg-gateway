// Package sshrelay tracks live supervisor sessions and opens per-request byte
// relays into sandboxes (OpenShell supervisor relay). The gateway never dials a
// sandbox: supervisors connect out, and each channel is a fresh outbound data
// stream paired with a waiting client.
package sshrelay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/whaleshell/whaleshell-core/relayproto"
)

// ErrNotConnected means the sandbox supervisor has no live control stream.
var ErrNotConnected = errors.New("sandbox is not ready: supervisor relay not connected")

// ErrOpenTimeout means the supervisor did not dial back the data stream in time.
var ErrOpenTimeout = errors.New("supervisor relay did not open the channel in time")

// Hub is safe for concurrent use.
type Hub struct {
	OpenTimeout       time.Duration
	KeepaliveInterval time.Duration
	KeepaliveTimeout  time.Duration
	Log               *slog.Logger

	mu       sync.Mutex
	sessions map[string]*session
	pending  map[string]*pending
}

type session struct {
	sandbox string
	conn    net.Conn
	w       *relayproto.MessageWriter
	done    chan struct{}
	once    sync.Once
}

func (s *session) close() {
	s.once.Do(func() {
		close(s.done)
		_ = s.conn.Close()
	})
}

type pending struct {
	sandbox string
	ready   chan net.Conn
}

// NewHub returns a hub with OpenShell-aligned keepalive defaults.
func NewHub() *Hub {
	return &Hub{
		OpenTimeout:       10 * time.Second,
		KeepaliveInterval: relayproto.KeepaliveInterval,
		KeepaliveTimeout:  relayproto.KeepaliveTimeout,
		sessions:          map[string]*session{},
		pending:           map[string]*pending{},
	}
}

func (h *Hub) log() *slog.Logger {
	if h.Log != nil {
		return h.Log
	}
	return slog.Default()
}

// Connected reports whether sandbox has a live supervisor control stream.
func (h *Hub) Connected(sandbox string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.sessions[sandbox]
	return ok
}

// Disconnect drops the supervisor session for sandbox (e.g. on delete).
func (h *Hub) Disconnect(sandbox string) {
	h.mu.Lock()
	s := h.sessions[sandbox]
	delete(h.sessions, sandbox)
	h.mu.Unlock()
	if s != nil {
		s.close()
	}
}

// ServeSupervisor upgrades r into the control stream for an already
// authenticated sandbox and blocks until the stream ends.
func (h *Hub) ServeSupervisor(w http.ResponseWriter, r *http.Request, sandbox string) {
	conn, err := relayproto.Accept(w, r)
	if err != nil {
		return
	}
	s := &session{sandbox: sandbox, conn: conn, w: relayproto.NewMessageWriter(conn), done: make(chan struct{})}
	h.mu.Lock()
	prev := h.sessions[sandbox]
	h.sessions[sandbox] = s
	h.mu.Unlock()
	if prev != nil {
		prev.close()
	}
	log := h.log().With(slog.String("op", "gateway.relay.supervisor"), slog.String("sandbox", sandbox))
	log.Info("supervisor connected")
	defer func() {
		h.mu.Lock()
		if h.sessions[sandbox] == s {
			delete(h.sessions, sandbox)
		}
		h.mu.Unlock()
		s.close()
		log.Info("supervisor disconnected")
	}()

	seen := make(chan struct{}, 1)
	go func() {
		rd := relayproto.NewMessageReader(conn)
		for {
			m, err := rd.Read()
			if err != nil {
				s.close()
				return
			}
			if m.Type == relayproto.MsgPong || m.Type == relayproto.MsgHello {
				select {
				case seen <- struct{}{}:
				default:
				}
			}
		}
	}()

	tick := time.NewTicker(h.KeepaliveInterval)
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case <-s.done:
			return
		case <-r.Context().Done():
			return
		case <-seen:
			last = time.Now()
		case <-tick.C:
			if time.Since(last) > h.KeepaliveTimeout {
				log.Warn("supervisor keepalive timeout")
				return
			}
			if err := s.w.Write(relayproto.Message{Type: relayproto.MsgPing}); err != nil {
				return
			}
		}
	}
}

// OpenChannel asks the sandbox supervisor for a new data stream to target and
// returns it once the supervisor dials back.
func (h *Hub) OpenChannel(ctx context.Context, sandbox, target string) (net.Conn, error) {
	id, err := newChannelID()
	if err != nil {
		return nil, err
	}
	p := &pending{sandbox: sandbox, ready: make(chan net.Conn, 1)}
	h.mu.Lock()
	s := h.sessions[sandbox]
	if s == nil {
		h.mu.Unlock()
		return nil, ErrNotConnected
	}
	h.pending[id] = p
	h.mu.Unlock()
	delivered := false
	defer func() {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
		if !delivered {
			select {
			case c := <-p.ready:
				_ = c.Close()
			default:
			}
		}
	}()
	if err := s.w.Write(relayproto.Message{Type: relayproto.MsgOpen, Channel: id, Target: target}); err != nil {
		s.close()
		return nil, ErrNotConnected
	}
	timeout := h.OpenTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case c := <-p.ready:
		delivered = true
		return c, nil
	case <-s.done:
		return nil, ErrNotConnected
	case <-t.C:
		return nil, ErrOpenTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ServeRelay upgrades the supervisor data stream for channel and hands it to
// the waiting OpenChannel caller. The channel must belong to sandbox.
func (h *Hub) ServeRelay(w http.ResponseWriter, r *http.Request, sandbox, channel string) {
	h.mu.Lock()
	p := h.pending[channel]
	if p != nil && p.sandbox == sandbox {
		delete(h.pending, channel)
	} else {
		p = nil
	}
	h.mu.Unlock()
	if p == nil {
		http.Error(w, "unknown relay channel", http.StatusNotFound)
		return
	}
	conn, err := relayproto.Accept(w, r)
	if err != nil {
		return
	}
	select {
	case p.ready <- conn:
	default:
		_ = conn.Close()
	}
}

func newChannelID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Bridge pipes client <-> supervisor until both sides finish.
func Bridge(client, supervisor io.ReadWriteCloser) {
	relayproto.Pipe(client, supervisor)
}
