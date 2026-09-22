// Package config loads gateway process configuration from the environment.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Config is process-level gateway configuration.
type Config struct {
	Listen   string
	DataDir  string
	TLSCert  string
	TLSKey   string
	LogLevel string
}

// Load reads WHALESHELL_* and OS defaults. CLI flags override in app/gateway.
func Load() Config {
	cfg := Config{
		Listen:   strings.TrimSpace(os.Getenv("WHALESHELL_GATEWAY_LISTEN")),
		DataDir:  strings.TrimSpace(os.Getenv("WHALESHELL_GATEWAY_DATA")),
		TLSCert:  strings.TrimSpace(os.Getenv("WHALESHELL_GATEWAY_TLS_CERT")),
		TLSKey:   strings.TrimSpace(os.Getenv("WHALESHELL_GATEWAY_TLS_KEY")),
		LogLevel: strings.TrimSpace(os.Getenv("WHALESHELL_LOG_LEVEL")),
	}
	if cfg.DataDir == "" {
		home, _ := os.UserHomeDir()
		cfg.DataDir = filepath.Join(home, ".local", "share", "whaleshell-gateway")
	}
	return cfg
}
