package conf

import (
	"fmt"
	"net"
)

// Debug configures optional debug endpoints. Keep these bound to localhost unless you
// explicitly protect them (firewall/VPN) because they may expose runtime internals.
type Debug struct {
	// Pprof enables the Go pprof HTTP endpoints when set (e.g. "127.0.0.1:6060").
	Pprof string `yaml:"pprof"`
}

func (d *Debug) setDefaults() {}

func (d *Debug) validate() []error {
	var errors []error
	if d.Pprof == "" {
		return errors
	}
	if _, err := net.ResolveTCPAddr("tcp", d.Pprof); err != nil {
		errors = append(errors, fmt.Errorf("debug pprof address '%s' is invalid: %v", d.Pprof, err))
	}
	return errors
}


