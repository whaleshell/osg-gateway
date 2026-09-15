// Package store persists osg-gateway registry state as JSON on disk.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State is durable gateway registry state (JSON on disk).
type State struct {
	GatewayID        string                    `json:"gateway_id"`
	UpdatedAt        time.Time                 `json:"updated_at"`
	Sandboxes        map[string]Sandbox        `json:"sandboxes"`
	Labels           map[string]string         `json:"labels,omitempty"`
	GlobalPolicyYAML string                    `json:"global_policy_yaml,omitempty"`
	Profiles         map[string]ProfileRecord  `json:"profiles,omitempty"`
	Providers        map[string]ProviderRecord `json:"providers,omitempty"`
}

// Sandbox is one registered sandbox record.
type Sandbox struct {
	Name              string            `json:"name"`
	ID                string            `json:"id,omitempty"`
	Image             string            `json:"image,omitempty"`
	Network           string            `json:"network,omitempty"`
	Status            string            `json:"status,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	BasePolicyYAML    string            `json:"base_policy_yaml,omitempty"`
	AttachedProviders []string          `json:"attached_providers,omitempty"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// ProfileRecord is a custom (imported) provider profile stored as YAML.
type ProfileRecord struct {
	ID   string `json:"id"`
	YAML string `json:"yaml"`
}

// ProviderRecord is a named instance referencing a profile.
// EnvVars are key names only; values live in the encrypted secrets store.
type ProviderRecord struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	EnvVars []string `json:"env_vars,omitempty"`
}

// Store persists State under DataDir/state.json.
type Store struct {
	mu      sync.Mutex
	DataDir string
	path    string
	state   State
}

// Open loads or initializes state in dataDir.
func Open(dataDir, gatewayID string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		DataDir: dataDir,
		path:    filepath.Join(dataDir, "state.json"),
		state: State{
			GatewayID: gatewayID,
			Sandboxes: map[string]Sandbox{},
			Labels:    map[string]string{},
			Profiles:  map[string]ProfileRecord{},
			Providers: map[string]ProviderRecord{},
		},
	}
	b, err := os.ReadFile(s.path)
	if err == nil {
		if err := json.Unmarshal(b, &s.state); err != nil {
			return nil, fmt.Errorf("gateway store: parse: %w", err)
		}
		if s.state.Sandboxes == nil {
			s.state.Sandboxes = map[string]Sandbox{}
		}
		if s.state.Profiles == nil {
			s.state.Profiles = map[string]ProfileRecord{}
		}
		if s.state.Providers == nil {
			s.state.Providers = map[string]ProviderRecord{}
		}
		if s.state.GatewayID == "" {
			s.state.GatewayID = gatewayID
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return s, s.flushLocked()
}

func (s *Store) flushLocked() error {
	s.state.UpdatedAt = time.Now().UTC()
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Snapshot returns a copy of current state.
func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.state
	out.Sandboxes = map[string]Sandbox{}
	for k, v := range s.state.Sandboxes {
		out.Sandboxes[k] = v
	}
	out.Profiles = map[string]ProfileRecord{}
	for k, v := range s.state.Profiles {
		out.Profiles[k] = v
	}
	out.Providers = map[string]ProviderRecord{}
	for k, v := range s.state.Providers {
		out.Providers[k] = v
	}
	return out
}

// UpsertSandbox records or updates a sandbox.
func (s *Store) UpsertSandbox(sb Sandbox) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.state.Sandboxes[sb.Name]; ok {
		if len(sb.AttachedProviders) == 0 && len(prev.AttachedProviders) > 0 {
			sb.AttachedProviders = prev.AttachedProviders
		}
		if sb.BasePolicyYAML == "" && prev.BasePolicyYAML != "" {
			sb.BasePolicyYAML = prev.BasePolicyYAML
		}
	}
	sb.UpdatedAt = time.Now().UTC()
	s.state.Sandboxes[sb.Name] = sb
	return s.flushLocked()
}

// DeleteSandbox removes a sandbox by name.
func (s *Store) DeleteSandbox(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Sandboxes, name)
	return s.flushLocked()
}

// GetSandbox returns a sandbox if present.
func (s *Store) GetSandbox(name string) (Sandbox, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sb, ok := s.state.Sandboxes[name]
	return sb, ok
}

// GetGlobalPolicy returns the stored global policy YAML (may be empty).
func (s *Store) GetGlobalPolicy() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.GlobalPolicyYAML
}

// SetGlobalPolicy stores global policy YAML bytes as a string.
func (s *Store) SetGlobalPolicy(yaml string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.GlobalPolicyYAML = yaml
	return s.flushLocked()
}

// UpsertProfile stores a custom profile YAML.
func (s *Store) UpsertProfile(id, yaml string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Profiles == nil {
		s.state.Profiles = map[string]ProfileRecord{}
	}
	s.state.Profiles[id] = ProfileRecord{ID: id, YAML: yaml}
	return s.flushLocked()
}

// DeleteProfile removes a custom profile.
func (s *Store) DeleteProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Profiles, id)
	return s.flushLocked()
}

// GetProfile returns a custom profile if present.
func (s *Store) GetProfile(id string) (ProfileRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[id]
	return p, ok
}

// UpsertProvider stores a provider instance (env refs only).
func (s *Store) UpsertProvider(rec ProviderRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Providers == nil {
		s.state.Providers = map[string]ProviderRecord{}
	}
	s.state.Providers[rec.Name] = rec
	return s.flushLocked()
}

// DeleteProvider removes a provider instance.
func (s *Store) DeleteProvider(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Providers, name)
	return s.flushLocked()
}

// GetProvider returns a provider instance.
func (s *Store) GetProvider(name string) (ProviderRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Providers[name]
	return p, ok
}

// AttachProvider appends a provider name to a sandbox attachment list.
func (s *Store) AttachProvider(sandbox, provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sb, ok := s.state.Sandboxes[sandbox]
	if !ok {
		return fmt.Errorf("sandbox %q not found", sandbox)
	}
	for _, p := range sb.AttachedProviders {
		if p == provider {
			return nil
		}
	}
	sb.AttachedProviders = append(sb.AttachedProviders, provider)
	sb.UpdatedAt = time.Now().UTC()
	s.state.Sandboxes[sandbox] = sb
	return s.flushLocked()
}

// DetachProvider removes a provider from a sandbox attachment list.
func (s *Store) DetachProvider(sandbox, provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sb, ok := s.state.Sandboxes[sandbox]
	if !ok {
		return fmt.Errorf("sandbox %q not found", sandbox)
	}
	out := sb.AttachedProviders[:0]
	for _, p := range sb.AttachedProviders {
		if p != provider {
			out = append(out, p)
		}
	}
	sb.AttachedProviders = out
	sb.UpdatedAt = time.Now().UTC()
	s.state.Sandboxes[sandbox] = sb
	return s.flushLocked()
}
