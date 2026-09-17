// Package gateway is the control-plane daemon (P8).
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zorneth/osg-core/defaults"
	"github.com/zorneth/osg-core/policy"
	"github.com/zorneth/osg-gateway/internal/logbuf"
	"github.com/zorneth/osg-gateway/internal/relay"
	"github.com/zorneth/osg-gateway/internal/store"
	"github.com/zorneth/osg-runtime/secrets"
)

// Options configure the HTTP(S) control plane.
type Options struct {
	Listen  string // default defaults.GatewayListen
	DataDir string // durable state
	TLSCert string // optional
	TLSKey  string // optional
	OIDC    OIDCOptions
}

// Run starts the gateway until context cancel / signal via ListenAndServe.
func Run(args []string) error {
	opt := Options{
		Listen:  defaults.GatewayListen,
		DataDir: defaultDataDir(),
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--listen":
			i++
			if i >= len(args) {
				return fmt.Errorf("--listen needs a value")
			}
			opt.Listen = args[i]
		case "--data-dir":
			i++
			if i >= len(args) {
				return fmt.Errorf("--data-dir needs a value")
			}
			opt.DataDir = args[i]
		case "--tls-cert":
			i++
			if i >= len(args) {
				return fmt.Errorf("--tls-cert needs a value")
			}
			opt.TLSCert = args[i]
		case "--tls-key":
			i++
			if i >= len(args) {
				return fmt.Errorf("--tls-key needs a value")
			}
			opt.TLSKey = args[i]
		case "--oidc-issuer":
			i++
			if i >= len(args) {
				return fmt.Errorf("--oidc-issuer needs a value")
			}
			opt.OIDC.Issuer = args[i]
		case "--oidc-audience":
			i++
			if i >= len(args) {
				return fmt.Errorf("--oidc-audience needs a value")
			}
			opt.OIDC.Audience = args[i]
		case "--oidc-client-id":
			i++
			if i >= len(args) {
				return fmt.Errorf("--oidc-client-id needs a value")
			}
			opt.OIDC.ClientID = args[i]
		case "--oidc-allow-insecure-http":
			opt.OIDC.AllowInsecureHTTP = true
		case "-h", "--help":
			fmt.Fprintf(os.Stderr, "usage: osg-gateway [--listen ADDR] [--data-dir DIR] [--tls-cert F] [--tls-key F]\n")
			fmt.Fprintf(os.Stderr, "                 [--oidc-issuer URL] [--oidc-client-id ID] [--oidc-audience AUD]\n")
			fmt.Fprintf(os.Stderr, "                 [--oidc-allow-insecure-http]\n")
			return nil
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	oidcFromEnvAndFlags(&opt)
	return Serve(context.Background(), opt)
}

func defaultDataDir() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "osg", "gateway")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "osg-gateway")
	}
	return filepath.Join(home, ".local", "state", "osg", "gateway")
}

func newGatewayID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "gw-" + hex.EncodeToString(b[:])
}

// Serve runs the HTTP API.
func Serve(ctx context.Context, opt Options) error {
	if opt.Listen == "" {
		opt.Listen = defaults.GatewayListen
	}
	if opt.DataDir == "" {
		opt.DataDir = defaultDataDir()
	}
	st, err := store.Open(opt.DataDir, newGatewayID())
	if err != nil {
		return err
	}
	sec, err := secrets.OpenLocal(opt.DataDir)
	if err != nil {
		return fmt.Errorf("gateway secrets: %w", err)
	}
	oidcFromEnvAndFlags(&opt)
	oidcValidator, err := newOIDCValidator(opt.OIDC)
	if err != nil {
		return fmt.Errorf("gateway oidc: %w", err)
	}
	logs := logbuf.NewHub(4096)
	hub := relay.NewHub()
	mux := http.NewServeMux()
	hub.Mount(mux)

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		s := st.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":         true,
			"gateway_id": s.GatewayID,
			"time":       time.Now().UTC(),
		})
	})
	mux.HandleFunc("/v1/info", func(w http.ResponseWriter, _ *http.Request) {
		s := st.Snapshot()
		kek := secrets.Inspect(opt.DataDir, os.Getenv)
		authMode := "local-dev"
		if oidcValidator != nil {
			authMode = "oidc"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"gateway_id":        s.GatewayID,
			"sandbox_count":     len(s.Sandboxes),
			"updated_at":        s.UpdatedAt,
			"data_dir":          opt.DataDir,
			"auth_mode":         authMode,
			"oidc_issuer":       opt.OIDC.Issuer,
			"host_osg_internal": "host.osg.internal → host-gateway (Docker)",
			"relay":             "long-poll /v1/relay/{name}/poll|exec|result",
			"secrets_kek": map[string]any{
				"source":  string(kek.Source),
				"pinned":  kek.Pinned,
				"env":     secrets.EnvKEK,
				"warning": kek.Warning(),
			},
		})
	})
	mux.HandleFunc("/v1/sandboxes", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s := st.Snapshot()
			list := make([]store.Sandbox, 0, len(s.Sandboxes))
			for _, sb := range s.Sandboxes {
				list = append(list, sb)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"sandboxes": list})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/v1/sandboxes/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/v1/sandboxes/")
		rest = strings.Trim(rest, "/")
		if rest == "" {
			http.Error(w, "bad name", http.StatusBadRequest)
			return
		}
		name, sub, hasSub := strings.Cut(rest, "/")
		if name == "" {
			http.Error(w, "bad name", http.StatusBadRequest)
			return
		}
		if hasSub {
			handleSandboxSubpath(w, r, st, sec, logs, BuiltinProvidersDir(), name, sub)
			return
		}
		switch r.Method {
		case http.MethodGet:
			sb, ok := st.GetSandbox(name)
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sb)
		case http.MethodPut:
			var sb store.Sandbox
			if err := json.NewDecoder(r.Body).Decode(&sb); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			sb.Name = name
			if err := st.UpsertSandbox(sb); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			if err := st.DeleteSandbox(name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mountProviderAPI(mux, st, sec, BuiltinProvidersDir())
	mountParityAPI(mux, st, oidcValidator)
	mountOIDCAuthAPI(mux, opt.OIDC, oidcValidator, st.AuthToken)
	if oidcValidator != nil {
		fmt.Fprintf(os.Stderr, "osg-gateway: OIDC auth enabled issuer=%s\n", opt.OIDC.Issuer)
	}
	// Fleet logs: GET /v1/logs?follow=1 lists all sandboxes' ring buffers via query names=
	mux.HandleFunc("/v1/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		names := r.URL.Query()["name"]
		if all := r.URL.Query().Get("all"); all == "1" || all == "true" {
			snap := st.Snapshot()
			names = names[:0]
			for n := range snap.Sandboxes {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			http.Error(w, "usage: /v1/logs?name=a&name=b or ?all=1", http.StatusBadRequest)
			return
		}
		// Snapshot merge (non-follow) for simplicity; follow uses per-sandbox SSE.
		follow := r.URL.Query().Get("follow") == "1" || r.URL.Query().Get("follow") == "true"
		if follow && len(names) == 1 {
			handleSandboxLogs(w, r, logs, names[0])
			return
		}
		if follow {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming unsupported", http.StatusInternalServerError)
				return
			}
			last := map[string]time.Time{}
			for {
				for _, n := range names {
					for _, ln := range logs.Snapshot(n, last[n], "", "", 0) {
						if !last[n].IsZero() && !ln.TS.After(last[n]) {
							continue
						}
						fmt.Fprintf(w, "data: [%s] %s\n\n", n, formatLogLine(ln))
						last[n] = ln.TS
					}
				}
				flusher.Flush()
				select {
				case <-r.Context().Done():
					return
				case <-time.After(500 * time.Millisecond):
				}
			}
		}
		var all []map[string]any
		for _, n := range names {
			for _, ln := range logs.Snapshot(n, time.Time{}, "", "", 200) {
				all = append(all, map[string]any{"sandbox": n, "line": ln})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"lines": all})
	})
	mux.HandleFunc("/v1/policy/global", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			yaml := st.GetGlobalPolicy()
			w.Header().Set("Content-Type", "application/yaml")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(yaml))
		case http.MethodPut:
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			doc, err := policy.Parse(body)
			if err != nil {
				http.Error(w, "invalid policy: "+err.Error(), http.StatusBadRequest)
				return
			}
			if err := doc.Validate(); err != nil {
				http.Error(w, "invalid policy: "+err.Error(), http.StatusBadRequest)
				return
			}
			if err := st.SetGlobalPolicy(string(body)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	srv := &http.Server{
		Addr:              opt.Listen,
		Handler:           withEdgeRouter(mux, st),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ln, err := net.Listen("tcp", opt.Listen)
	if err != nil {
		return fmt.Errorf("gateway listen %s: %w", opt.Listen, err)
	}
	fmt.Fprintf(os.Stderr, "osg-gateway: listening on http://%s (data=%s id=%s)\n",
		opt.Listen, opt.DataDir, st.Snapshot().GatewayID)
	if opt.TLSCert != "" && opt.TLSKey != "" {
		fmt.Fprintf(os.Stderr, "osg-gateway: TLS enabled\n")
		errCh := make(chan error, 1)
		go func() { errCh <- srv.ServeTLS(ln, opt.TLSCert, opt.TLSKey) }()
		select {
		case <-ctx.Done():
			_ = srv.Shutdown(context.Background())
			return ctx.Err()
		case err := <-errCh:
			if err == http.ErrServerClosed {
				return nil
			}
			return err
		}
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
