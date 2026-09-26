package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/whaleshell/whaleshell-runtime/idp"
)

// OIDCOptions configure issuer-backed JWT auth (optional).
type OIDCOptions struct {
	Issuer            string
	Audience          string
	ClientID          string // advertised to CLI via /v1/auth/oidc
	AllowInsecureHTTP bool
}

func oidcFromEnvAndFlags(opt *Options) {
	if opt.OIDC.Issuer == "" {
		opt.OIDC.Issuer = strings.TrimSpace(os.Getenv("WHALESHELL_OIDC_ISSUER"))
	}
	if opt.OIDC.Audience == "" {
		opt.OIDC.Audience = strings.TrimSpace(os.Getenv("WHALESHELL_OIDC_AUDIENCE"))
	}
	if opt.OIDC.ClientID == "" {
		opt.OIDC.ClientID = strings.TrimSpace(firstNonEmptyEnv("WHALESHELL_OIDC_CLIENT_ID", "OPENSHELL_OIDC_CLIENT_ID"))
	}
	if !opt.OIDC.AllowInsecureHTTP {
		v := strings.ToLower(strings.TrimSpace(os.Getenv("WHALESHELL_OIDC_ALLOW_INSECURE_HTTP")))
		opt.OIDC.AllowInsecureHTTP = v == "1" || v == "true" || v == "yes"
	}
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func newOIDCValidator(o OIDCOptions) (*idp.OIDC, error) {
	if strings.TrimSpace(o.Issuer) == "" {
		return nil, nil
	}
	return idp.NewOIDC(idp.OIDCConfig{
		Issuer:            o.Issuer,
		Audience:          o.Audience,
		AllowInsecureHTTP: o.AllowInsecureHTTP,
	})
}

func mountOIDCAuthAPI(mux *http.ServeMux, oidcCfg OIDCOptions, validator *idp.OIDC, localToken func() string) {
	mux.HandleFunc("/v1/auth/oidc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(oidcCfg.Issuer) == "" {
			http.Error(w, "oidc not configured", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":              oidcCfg.Issuer,
			"audience":            oidcCfg.Audience,
			"client_id":           oidcCfg.ClientID,
			"allow_insecure_http": oidcCfg.AllowInsecureHTTP,
			"mode":                "oidc",
		})
	})

	_ = validator
	_ = localToken
}
