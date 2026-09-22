// Package gateway wires the control-plane application.
package gateway

import (
	"github.com/whaleshell/whaleshell-gateway/config"
	"github.com/whaleshell/whaleshell-gateway/internal/httpapi"
)

// Run is the composition root for whaleshell-gateway.
func Run(args []string) error {
	_ = config.Load()
	return httpapi.Run(args)
}
