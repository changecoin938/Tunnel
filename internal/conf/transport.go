package conf

import (
	"fmt"
	"runtime"
	"slices"
)

type Transport struct {
	Protocol string `yaml:"protocol"`
	Conn     int    `yaml:"conn"`
	KCP      *KCP   `yaml:"kcp"`
}

func (t *Transport) setDefaults(role string) {
	if t.Conn == 0 {
		// Default to NumCPU pcap handles. Each handle has its own kernel buffer
		// and lock, so more handles = less contention under high concurrency.
		// On a 4-core server with 500 users, 4 handles reduce per-handle load
		// from 250 users/handle (conn=2) to 125 users/handle.
		t.Conn = runtime.NumCPU()
		if t.Conn < 2 {
			t.Conn = 2
		}
		if t.Conn > 16 {
			t.Conn = 16
		}
	}
	switch t.Protocol {
	case "kcp":
		if t.KCP == nil {
			t.KCP = &KCP{}
		}
		t.KCP.setDefaults(role, t.Conn)
	}
}

func (t *Transport) validate() []error {
	var errors []error

	validProtocols := []string{"kcp"}
	if !slices.Contains(validProtocols, t.Protocol) {
		errors = append(errors, fmt.Errorf("transport protocol must be one of: %v", validProtocols))
	}

	if t.Conn < 1 || t.Conn > 256 {
		errors = append(errors, fmt.Errorf("KCP conn must be between 1-256 connections"))
	}

	switch t.Protocol {
	case "kcp":
		if t.KCP == nil {
			errors = append(errors, fmt.Errorf("transport.kcp is required when transport.protocol is 'kcp'"))
			break
		}
		errors = append(errors, t.KCP.validate()...)
	}

	return errors
}
