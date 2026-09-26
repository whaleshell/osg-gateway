package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AuthTokenFile is the owner-only file holding the local-dev bearer token
// (OpenShell keeps local mTLS material on disk the same way).
const AuthTokenFile = "auth_token"

// SandboxToken authenticates one sandbox supervisor (proxy sidecar).
type SandboxToken struct {
	Hash       string `json:"hash"`
	IssuedAtMS int64  `json:"issued_at_ms"`
}

// SSHSession is an OpenShell-style SSH session record.
type SSHSession struct {
	ID          string `json:"id"`
	Sandbox     string `json:"sandbox"`
	TokenHash   string `json:"token_hash"`
	Subject     string `json:"subject,omitempty"`
	CreatedAtMS int64  `json:"created_at_ms"`
	ExpiresAtMS int64  `json:"expires_at_ms,omitempty"` // 0 = no expiry
	Revoked     bool   `json:"revoked,omitempty"`
}

// SSH session validation errors.
var (
	ErrSessionNotFound = errors.New("ssh session not found")
	ErrSessionRevoked  = errors.New("ssh session revoked")
	ErrSessionExpired  = errors.New("ssh session expired")
	ErrSessionSandbox  = errors.New("ssh session does not match sandbox")
)

func newSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// HashToken returns the stored form of a bearer secret.
func HashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func hashEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// WriteAuthTokenFile ensures the local-dev token exists and mirrors it to
// DataDir/auth_token (0600) so a local CLI can authenticate without a
// network login round-trip.
func (s *Store) WriteAuthTokenFile() (string, error) {
	tok, err := s.EnsureAuthToken()
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.DataDir, AuthTokenFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

// IssueSandboxToken rotates the supervisor token for an existing sandbox and
// returns the plaintext once. Only the hash is persisted.
func (s *Store) IssueSandboxToken(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.Sandboxes[name]; !ok {
		return "", fmt.Errorf("sandbox %q not found", name)
	}
	tok, err := newSecret()
	if err != nil {
		return "", err
	}
	if s.state.SandboxTokens == nil {
		s.state.SandboxTokens = map[string]SandboxToken{}
	}
	s.state.SandboxTokens[name] = SandboxToken{Hash: HashToken(tok), IssuedAtMS: time.Now().UnixMilli()}
	if err := s.flushLocked(); err != nil {
		return "", err
	}
	return tok, nil
}

// SandboxForToken resolves a supervisor token to its sandbox name.
func (s *Store) SandboxForToken(tok string) (string, bool) {
	if tok == "" {
		return "", false
	}
	h := HashToken(tok)
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, rec := range s.state.SandboxTokens {
		if hashEqual(rec.Hash, h) {
			if _, ok := s.state.Sandboxes[name]; !ok {
				return "", false
			}
			return name, true
		}
	}
	return "", false
}

// CreateSSHSession mints a session token bound to sandbox. ttl <= 0 means no expiry.
func (s *Store) CreateSSHSession(sandbox, subject string, ttl time.Duration) (SSHSession, string, error) {
	tok, err := newSecret()
	if err != nil {
		return SSHSession{}, "", err
	}
	idRaw, err := newSecret()
	if err != nil {
		return SSHSession{}, "", err
	}
	now := time.Now()
	sess := SSHSession{
		ID:          "ssh-" + idRaw[:16],
		Sandbox:     sandbox,
		TokenHash:   HashToken(tok),
		Subject:     subject,
		CreatedAtMS: now.UnixMilli(),
	}
	if ttl > 0 {
		sess.ExpiresAtMS = now.Add(ttl).UnixMilli()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.Sandboxes[sandbox]; !ok {
		return SSHSession{}, "", fmt.Errorf("sandbox %q not found", sandbox)
	}
	if s.state.SSHSessions == nil {
		s.state.SSHSessions = map[string]SSHSession{}
	}
	s.state.SSHSessions[sess.ID] = sess
	if err := s.flushLocked(); err != nil {
		return SSHSession{}, "", err
	}
	return sess, tok, nil
}

// ValidateSSHSession checks a session token against the requested sandbox.
func (s *Store) ValidateSSHSession(tok, sandbox string, now time.Time) (SSHSession, error) {
	if tok == "" {
		return SSHSession{}, ErrSessionNotFound
	}
	h := HashToken(tok)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.state.SSHSessions {
		if !hashEqual(sess.TokenHash, h) {
			continue
		}
		switch {
		case sess.Revoked:
			return sess, ErrSessionRevoked
		case sess.ExpiresAtMS > 0 && now.UnixMilli() >= sess.ExpiresAtMS:
			return sess, ErrSessionExpired
		case sess.Sandbox != sandbox:
			return sess, ErrSessionSandbox
		}
		if _, ok := s.state.Sandboxes[sess.Sandbox]; !ok {
			return sess, ErrSessionNotFound
		}
		return sess, nil
	}
	return SSHSession{}, ErrSessionNotFound
}

// RevokeSSHSession marks a session revoked by id.
func (s *Store) RevokeSSHSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.state.SSHSessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	sess.Revoked = true
	s.state.SSHSessions[id] = sess
	return s.flushLocked()
}

// ListSSHSessions returns sessions for sandbox ("" = all).
func (s *Store) ListSSHSessions(sandbox string) []SSHSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SSHSession, 0, len(s.state.SSHSessions))
	for _, sess := range s.state.SSHSessions {
		if sandbox == "" || sess.Sandbox == sandbox {
			out = append(out, sess)
		}
	}
	return out
}

// ReapSSHSessions deletes expired and revoked sessions; returns how many.
func (s *Store) ReapSSHSessions(now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, sess := range s.state.SSHSessions {
		if sess.Revoked || (sess.ExpiresAtMS > 0 && now.UnixMilli() >= sess.ExpiresAtMS) {
			delete(s.state.SSHSessions, id)
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.flushLocked()
}
