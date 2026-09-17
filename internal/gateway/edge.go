package gateway

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/zorneth/osg-gateway/internal/store"
)

const (
	edgeSuffixOpenShell = ".openshell.localhost"
	edgeSuffixOSG       = ".osg.localhost"
)

// withEdgeRouter proxies Host *.openshell.localhost / *.osg.localhost to registered services.
// Other requests fall through to the control-plane mux (.localhost resolves to 127.0.0.1).
func withEdgeRouter(next http.Handler, st *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		name, ok := edgeServiceName(host)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		svc, found := st.GetService(name)
		if !found {
			http.Error(w, fmt.Sprintf("service %q not exposed", name), http.StatusNotFound)
			return
		}
		backendHost := strings.TrimSpace(svc.BackendHost)
		backendPort := svc.BackendPort
		if backendHost == "" {
			backendHost = "127.0.0.1"
		}
		if backendPort <= 0 {
			backendPort = svc.Port
		}
		if backendPort <= 0 {
			http.Error(w, "service has no backend port", http.StatusBadGateway)
			return
		}
		target, err := url.Parse(fmt.Sprintf("http://%s", net.JoinHostPort(backendHost, fmt.Sprintf("%d", backendPort))))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		origDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			origDirector(req)
			req.Host = target.Host
		}
		proxy.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, err error) {
			http.Error(rw, "edge proxy: "+err.Error(), http.StatusBadGateway)
		}
		proxy.ServeHTTP(w, r)
	})
}

func edgeServiceName(host string) (string, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, suf := range []string{edgeSuffixOpenShell, edgeSuffixOSG} {
		if strings.HasSuffix(host, suf) {
			name := strings.TrimSuffix(host, suf)
			if name == "" || strings.Contains(name, ".") {
				return "", false
			}
			return name, true
		}
	}
	return "", false
}
