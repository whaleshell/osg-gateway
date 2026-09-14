package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/zorneth/osg-core/policy"
	"github.com/zorneth/osg-core/provider"
	"github.com/zorneth/osg-gateway/internal/store"
	"gopkg.in/yaml.v3"
)

func mountProviderAPI(mux *http.ServeMux, st *store.Store, builtinDir string) {
	mux.HandleFunc("/v1/profiles", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		list := listProfiles(st, builtinDir)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"profiles": list})
	})
	mux.HandleFunc("/v1/profiles/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/profiles/"), "/")
		if id == "" || strings.Contains(id, "/") {
			http.Error(w, "bad id", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			p, src, err := resolveProfile(st, builtinDir, id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"profile": p, "source": src})
		case http.MethodPut:
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			p, err := provider.ParseYAML(body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if p.ID != id {
				http.Error(w, "id mismatch", http.StatusBadRequest)
				return
			}
			if err := st.UpsertProfile(id, string(body)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			if err := st.DeleteProfile(id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/v1/providers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s := st.Snapshot()
			list := make([]store.ProviderRecord, 0, len(s.Providers))
			for _, p := range s.Providers {
				list = append(list, p)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"providers": list})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/v1/providers/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/providers/"), "/")
		if name == "" || strings.Contains(name, "/") {
			http.Error(w, "bad name", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			p, ok := st.GetProvider(name)
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(p)
		case http.MethodPut:
			var rec store.ProviderRecord
			if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			rec.Name = name
			if strings.TrimSpace(rec.Type) == "" {
				http.Error(w, "type (profile id) required", http.StatusBadRequest)
				return
			}
			if _, _, err := resolveProfile(st, builtinDir, rec.Type); err != nil {
				http.Error(w, "unknown profile type: "+err.Error(), http.StatusBadRequest)
				return
			}
			if err := st.UpsertProvider(rec); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			if err := st.DeleteProvider(name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func handleSandboxSubpath(w http.ResponseWriter, r *http.Request, st *store.Store, builtinDir, name, rest string) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] == "effective-policy" && r.Method == http.MethodGet:
		doc, err := effectivePolicy(st, builtinDir, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		b, err := yaml.Marshal(doc)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(b)
	case len(parts) == 2 && parts[0] == "providers":
		prov := parts[1]
		switch r.Method {
		case http.MethodPut, http.MethodPost:
			if _, ok := st.GetProvider(prov); !ok {
				http.Error(w, "provider not found", http.StatusNotFound)
				return
			}
			if err := st.AttachProvider(name, prov); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			if err := st.DetachProvider(name, prov); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func listProfiles(st *store.Store, builtinDir string) []map[string]string {
	var out []map[string]string
	seen := map[string]struct{}{}
	if builtinDir != "" {
		if m, err := provider.LoadDir(builtinDir); err == nil {
			for id := range m {
				seen[id] = struct{}{}
				out = append(out, map[string]string{"id": id, "source": "builtin"})
			}
		}
	}
	for id := range st.Snapshot().Profiles {
		if _, ok := seen[id]; ok {
			continue
		}
		out = append(out, map[string]string{"id": id, "source": "custom"})
	}
	return out
}

func resolveProfile(st *store.Store, builtinDir, id string) (provider.Profile, string, error) {
	if rec, ok := st.GetProfile(id); ok {
		p, err := provider.ParseYAML([]byte(rec.YAML))
		return p, "custom", err
	}
	if builtinDir != "" {
		path := filepath.Join(builtinDir, id+".yaml")
		if _, err := os.Stat(path); err == nil {
			p, err := provider.LoadFile(path)
			return p, "builtin", err
		}
		path = filepath.Join(builtinDir, id+".yml")
		if _, err := os.Stat(path); err == nil {
			p, err := provider.LoadFile(path)
			return p, "builtin", err
		}
	}
	return provider.Profile{}, "", fmt.Errorf("profile %q not found", id)
}

func effectivePolicy(st *store.Store, builtinDir, sandbox string) (policy.Document, error) {
	sb, ok := st.GetSandbox(sandbox)
	if !ok {
		return policy.Document{}, fmt.Errorf("sandbox %q not found", sandbox)
	}
	var base policy.Document
	if strings.TrimSpace(sb.BasePolicyYAML) != "" {
		doc, err := policy.Parse([]byte(sb.BasePolicyYAML))
		if err != nil {
			return policy.Document{}, err
		}
		base = doc
	} else {
		base = policy.Document{Version: 1, Network: &policy.Network{Default: "deny"}}
	}
	globalYAML := st.GetGlobalPolicy()
	suppress := false
	if strings.TrimSpace(globalYAML) != "" {
		gdoc, err := policy.Parse([]byte(globalYAML))
		if err != nil {
			return policy.Document{}, err
		}
		if gdoc.Network != nil && len(gdoc.Network.Allow) > 0 {
			suppress = true
		}
		base, err = policy.MergeGlobal(base, gdoc)
		if err != nil {
			return policy.Document{}, err
		}
	}
	var layers []provider.Layer
	for _, name := range sb.AttachedProviders {
		inst, ok := st.GetProvider(name)
		if !ok {
			continue
		}
		prof, _, err := resolveProfile(st, builtinDir, inst.Type)
		if err != nil {
			return policy.Document{}, err
		}
		layers = append(layers, provider.Layer{
			InstanceName: name,
			Profile:      prof,
			EnvVars:      inst.EnvVars,
		})
	}
	out := provider.Compose(base, layers, suppress)
	if err := out.Validate(); err != nil {
		return policy.Document{}, err
	}
	return out, nil
}

// BuiltinProvidersDir tries to locate osg-cli/providers next to the module.
func BuiltinProvidersDir() string {
	return provider.FindBuiltinDir()
}
