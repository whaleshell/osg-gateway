package store

import (
	"fmt"
	"time"
)

// ServiceRecord is an exposed HTTP service routed via *.openshell.localhost.
type ServiceRecord struct {
	Name        string    `json:"name"`
	Sandbox     string    `json:"sandbox"`
	Port        int       `json:"port"` // guest/target port
	BackendHost string    `json:"backend_host"`
	BackendPort int       `json:"backend_port"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WorkspaceMember is a subject with a role in a workspace.
type WorkspaceMember struct {
	Subject string `json:"subject"`
	Role    string `json:"role"`
}

// WorkspaceRecord is a named workspace with members.
type WorkspaceRecord struct {
	Name      string            `json:"name"`
	Members   []WorkspaceMember `json:"members,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// UpsertService stores an exposed service.
func (s *Store) UpsertService(rec ServiceRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Services == nil {
		s.state.Services = map[string]ServiceRecord{}
	}
	rec.UpdatedAt = time.Now().UTC()
	s.state.Services[rec.Name] = rec
	return s.flushLocked()
}

// GetService returns a service by name.
func (s *Store) GetService(name string) (ServiceRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.state.Services[name]
	return rec, ok
}

// DeleteService removes a service.
func (s *Store) DeleteService(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Services, name)
	return s.flushLocked()
}

// ListServices returns all services.
func (s *Store) ListServices() []ServiceRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ServiceRecord, 0, len(s.state.Services))
	for _, rec := range s.state.Services {
		out = append(out, rec)
	}
	return out
}

// UpsertWorkspace stores a workspace.
func (s *Store) UpsertWorkspace(ws WorkspaceRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Workspaces == nil {
		s.state.Workspaces = map[string]WorkspaceRecord{}
	}
	now := time.Now().UTC()
	if ws.CreatedAt.IsZero() {
		if prev, ok := s.state.Workspaces[ws.Name]; ok && !prev.CreatedAt.IsZero() {
			ws.CreatedAt = prev.CreatedAt
		} else {
			ws.CreatedAt = now
		}
	}
	ws.UpdatedAt = now
	s.state.Workspaces[ws.Name] = ws
	return s.flushLocked()
}

// GetWorkspace returns a workspace.
func (s *Store) GetWorkspace(name string) (WorkspaceRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws, ok := s.state.Workspaces[name]
	return ws, ok
}

// DeleteWorkspace removes a workspace.
func (s *Store) DeleteWorkspace(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Workspaces, name)
	return s.flushLocked()
}

// ListWorkspaces returns all workspaces.
func (s *Store) ListWorkspaces() []WorkspaceRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]WorkspaceRecord, 0, len(s.state.Workspaces))
	for _, ws := range s.state.Workspaces {
		out = append(out, ws)
	}
	return out
}

// WorkspaceMemberUpsert adds or updates a member role.
func (s *Store) WorkspaceMemberUpsert(name, subject, role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws, ok := s.state.Workspaces[name]
	if !ok {
		return fmt.Errorf("workspace %q not found", name)
	}
	if role == "" {
		role = "user"
	}
	found := false
	for i := range ws.Members {
		if ws.Members[i].Subject == subject {
			ws.Members[i].Role = role
			found = true
			break
		}
	}
	if !found {
		ws.Members = append(ws.Members, WorkspaceMember{Subject: subject, Role: role})
	}
	ws.UpdatedAt = time.Now().UTC()
	s.state.Workspaces[name] = ws
	return s.flushLocked()
}

// WorkspaceMemberRemove deletes a member.
func (s *Store) WorkspaceMemberRemove(name, subject string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws, ok := s.state.Workspaces[name]
	if !ok {
		return fmt.Errorf("workspace %q not found", name)
	}
	out := ws.Members[:0]
	for _, m := range ws.Members {
		if m.Subject != subject {
			out = append(out, m)
		}
	}
	ws.Members = out
	ws.UpdatedAt = time.Now().UTC()
	s.state.Workspaces[name] = ws
	return s.flushLocked()
}
