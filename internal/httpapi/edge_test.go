package httpapi

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/whaleshell/whaleshell-gateway/internal/storage/store"
)

func TestEdgeServiceName(t *testing.T) {
	name, ok := edgeServiceName("web.openshell.localhost")
	if !ok || name != "web" {
		t.Fatalf("got %q %v", name, ok)
	}
	name, ok = edgeServiceName("api.whaleshell.localhost")
	if !ok || name != "api" {
		t.Fatalf("got %q %v", name, ok)
	}
	if _, ok := edgeServiceName("127.0.0.1"); ok {
		t.Fatal("expected no match")
	}
	if _, ok := edgeServiceName("a.b.openshell.localhost"); ok {
		t.Fatal("expected nested subdomain reject")
	}
}

func TestEdgeRouterProxies(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok:"+r.URL.Path)
	}))
	t.Cleanup(backend.Close)

	host, portStr, err := net.SplitHostPort(backend.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	if host == "::1" || host == "" {
		host = "127.0.0.1"
	}

	dir := t.TempDir()
	st, err := store.Open(dir, "test-gw")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertService(store.ServiceRecord{
		Name: "web", Sandbox: "sb", Port: port, BackendHost: host, BackendPort: port,
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := withEdgeRouter(mux, st)

	req := httptest.NewRequest(http.MethodGet, "http://web.openshell.localhost/hi", nil)
	req.Host = "web.openshell.localhost"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "ok:/hi" {
		t.Fatalf("body %q", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/healthz", nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("control plane status %d", rec2.Code)
	}
}

func TestOAuth2TokenParseGrant(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			http.Error(w, "bad grant", 400)
			return
		}
		if r.Form.Get("refresh_token") != "rt" {
			http.Error(w, "bad rt", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"at","expires_in":3600}`)
	}))
	t.Cleanup(ts.Close)

	tok, exp, err := oauth2Token(map[string]string{
		"token_url":     ts.URL,
		"refresh_token": "rt",
		"client_id":     "cid",
		"client_secret": "sec",
	}, "refresh_token")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "at" || exp <= 0 {
		t.Fatalf("tok=%q exp=%d", tok, exp)
	}
}
