package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/zorneth/osg-gateway/internal/store"
)

// rotateCredential performs strategy-specific credential refresh and returns
// the new secret value plus optional expiry (unix ms).
func rotateCredential(cfg store.ProviderRefreshConfig, key string) (value string, expiresAtMS int64, err error) {
	strategy := strings.TrimSpace(cfg.Strategy)
	if strategy == "" {
		strategy = "env"
	}
	switch strategy {
	case "env":
		v, ok := lookupEnv(key)
		if !ok {
			return "", 0, fmt.Errorf("env %s not set on gateway host", key)
		}
		return v, 0, nil
	case "oauth2-refresh-token":
		return oauth2Token(cfg.Material, "refresh_token")
	case "oauth2-client-credentials":
		return oauth2Token(cfg.Material, "client_credentials")
	case "aws-sts-assume-role":
		// MVP: read pre-fetched session token from material or host env.
		if v := strings.TrimSpace(cfg.Material["access_key_id"]); v != "" {
			secret := strings.TrimSpace(cfg.Material["secret_access_key"])
			token := strings.TrimSpace(cfg.Material["session_token"])
			if secret == "" {
				return "", 0, fmt.Errorf("aws-sts-assume-role: material secret_access_key required")
			}
			// Store as JSON blob under the credential key for sidecar rewrite.
			blob, _ := json.Marshal(map[string]string{
				"access_key_id":     v,
				"secret_access_key": secret,
				"session_token":     token,
			})
			return string(blob), cfg.ExpiresAtMS, nil
		}
		v, ok := lookupEnv(key)
		if !ok {
			return "", 0, fmt.Errorf("aws-sts-assume-role: set material access_key_id/secret_access_key or host env %s", key)
		}
		return v, 0, nil
	default:
		return "", 0, fmt.Errorf("unsupported refresh strategy %q", strategy)
	}
}

func lookupEnv(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return "", false
	}
	return v, true
}

// oauth2Token exchanges refresh_token or client_credentials at token_url.
func oauth2Token(material map[string]string, grant string) (string, int64, error) {
	if material == nil {
		return "", 0, fmt.Errorf("oauth2: material required")
	}
	tokenURL := strings.TrimSpace(firstNonEmpty(material["token_url"], material["token_uri"]))
	if tokenURL == "" {
		return "", 0, fmt.Errorf("oauth2: material token_url required")
	}
	clientID := strings.TrimSpace(material["client_id"])
	clientSecret := strings.TrimSpace(material["client_secret"])
	form := url.Values{}
	form.Set("grant_type", grant)
	switch grant {
	case "refresh_token":
		rt := strings.TrimSpace(firstNonEmpty(material["refresh_token"], material["refresh_token_value"]))
		if rt == "" {
			return "", 0, fmt.Errorf("oauth2-refresh-token: material refresh_token required")
		}
		form.Set("refresh_token", rt)
	case "client_credentials":
		// client_id/secret in body or Basic auth below
	default:
		return "", 0, fmt.Errorf("oauth2: unsupported grant %q", grant)
	}
	if scope := strings.TrimSpace(material["scope"]); scope != "" {
		form.Set("scope", scope)
	}
	if clientID != "" {
		form.Set("client_id", clientID)
	}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if clientID != "" && clientSecret != "" && material["auth_style"] == "basic" {
		req.SetBasicAuth(clientID, clientSecret)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("oauth2 token request: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return "", 0, fmt.Errorf("oauth2 token endpoint: %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", 0, fmt.Errorf("oauth2 token parse: %w", err)
	}
	if strings.TrimSpace(tok.AccessToken) == "" {
		return "", 0, fmt.Errorf("oauth2: empty access_token")
	}
	var expMS int64
	if tok.ExpiresIn > 0 {
		expMS = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).UnixMilli()
	}
	return tok.AccessToken, expMS, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
