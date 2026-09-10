// Package gateway is the control-plane daemon implementation (P8).
package gateway

import (
	"fmt"

	"github.com/lkmavi/osg-core"
)

// Run starts the gateway (stub).
func Run(_ []string) error {
	return fmt.Errorf("osg-gateway: %w", core.ErrNotImplemented)
}
