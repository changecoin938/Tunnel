package conf

// KCP holds configuration for the KCP transport and smux multiplexing.

import (
	"fmt"
	"slices"

	"github.com/xtaci/kcp-go/v5"
)

type KCP struct {
	Mode         string `yaml:"mode"`
	NoDelay      int    `yaml:"nodelay"`
	Interval     int    `yaml:"interval"`
	Resend       int    `yaml:"resend"`
	NoCongestion int    `yaml:"nocongestion"`
	WDelay       bool   `yaml:"wdelay"`
	AckNoDelay   bool   `yaml:"acknodelay"`

	MTU    int `yaml:"mtu"`
	Rcvwnd int `yaml:"rcvwnd"`
	Sndwnd int `yaml:"sndwnd"`
	Dshard int `yaml:"dshard"`
	Pshard int `yaml:"pshard"`

	Block_ string `yaml:"block"`
	Key    string `yaml:"key"`

	// Guard is a lightweight packet-level filter applied BEFORE KCP decrypt.
	// It prepends a short header and rejects packets that don't match.
	// This is useful to cheaply drop random junk / DoS traffic hitting the port.
	//
	// Note: Both client and server MUST have identical settings.
	Guard       *bool  `yaml:"guard"`
	GuardMagic  string `yaml:"guard_magic"`  // 4 bytes (e.g. "PQT1")
	GuardWindow int    `yaml:"guard_window"` // seconds (cookie rotation window)
	GuardSkew   int    `yaml:"guard_skew"`   // accepted previous windows

	// Defensive server-side limits. Use -1 for unlimited.
	MaxSessions          int `yaml:"max_sessions"`
	MaxStreamsTotal      int `yaml:"max_streams_total"`
	MaxStreamsPerSession int `yaml:"max_streams_per_session"`

	// HeaderTimeout is the deadline (in seconds) for reading the initial per-stream
	// protocol header (gob). This limits resource pinning by stalled clients.
	HeaderTimeout int `yaml:"header_timeout"`

	Smuxbuf   int `yaml:"smuxbuf"`
	Streambuf int `yaml:"streambuf"`

	Block kcp.BlockCrypt `yaml:"-"`
}

func (k *KCP) setDefaults(role string) {
	if k.Mode == "" {
		k.Mode = "fast"
	}
	if k.MTU == 0 {
		k.MTU = 1350
	}

	if k.Rcvwnd == 0 {
		if role == "server" {
			k.Rcvwnd = 1024
		} else {
			k.Rcvwnd = 512
		}
	}
	if k.Sndwnd == 0 {
		if role == "server" {
			k.Sndwnd = 1024
		} else {
			k.Sndwnd = 512
		}
	}

	// if k.Dshard == 0 {
	// 	k.Dshard = 10
	// }
	// if k.Pshard == 0 {
	// 	k.Pshard = 3
	// }

	if k.Block_ == "" {
		k.Block_ = "aes"
	}

	// Default hardening: enable guard unless explicitly disabled.
	if k.Guard == nil {
		v := true
		k.Guard = &v
	}
	if k.GuardMagic == "" {
		k.GuardMagic = "PQT1"
	}
	if k.GuardWindow == 0 {
		k.GuardWindow = 30
	}
	if k.GuardSkew == 0 {
		k.GuardSkew = 1
	}

	if k.HeaderTimeout == 0 {
		k.HeaderTimeout = 10
	}

	// Defensive defaults for servers. Use -1 in config to disable limits.
	if role == "server" {
		if k.MaxSessions == 0 {
			k.MaxSessions = 1024
		}
		if k.MaxStreamsTotal == 0 {
			k.MaxStreamsTotal = 32768
		}
		if k.MaxStreamsPerSession == 0 {
			k.MaxStreamsPerSession = 4096
		}
	}

	if k.Smuxbuf == 0 {
		k.Smuxbuf = 4 * 1024 * 1024
	}
	if k.Streambuf == 0 {
		k.Streambuf = 2 * 1024 * 1024
	}
}

func (k *KCP) validate() []error {
	var errors []error

	validModes := []string{"normal", "fast", "fast2", "fast3", "manual"}
	if !slices.Contains(validModes, k.Mode) {
		errors = append(errors, fmt.Errorf("KCP mode must be one of: %v", validModes))
	}

	if k.MTU < 50 || k.MTU > 1500 {
		errors = append(errors, fmt.Errorf("KCP MTU must be between 50-1500 bytes"))
	}

	if k.Rcvwnd < 1 || k.Rcvwnd > 65535 {
		errors = append(errors, fmt.Errorf("KCP rcvwnd must be between 1-65535"))
	}
	if k.Sndwnd < 1 || k.Sndwnd > 65535 {
		errors = append(errors, fmt.Errorf("KCP sndwnd must be between 1-65535"))
	}

	validBlocks := []string{"aes", "aes-128", "aes-128-gcm", "aes-192", "salsa20", "blowfish", "twofish", "cast5", "3des", "tea", "xtea", "xor", "sm4", "none", "null"}
	if !slices.Contains(validBlocks, k.Block_) {
		errors = append(errors, fmt.Errorf("KCP encryption block must be one of: %v", validBlocks))
	}
	if !slices.Contains([]string{"none", "null"}, k.Block_) && len(k.Key) == 0 {
		errors = append(errors, fmt.Errorf("KCP encryption key is required"))
	}
	b, err := newBlock(k.Block_, k.Key)
	if err != nil {
		errors = append(errors, err)
	}
	k.Block = b

	if k.Guard != nil && *k.Guard {
		if len(k.GuardMagic) != 4 {
			errors = append(errors, fmt.Errorf("KCP guard_magic must be exactly 4 bytes"))
		}
		if k.GuardWindow < 1 || k.GuardWindow > 3600 {
			errors = append(errors, fmt.Errorf("KCP guard_window must be between 1-3600 seconds"))
		}
		if k.GuardSkew < 0 || k.GuardSkew > 10 {
			errors = append(errors, fmt.Errorf("KCP guard_skew must be between 0-10 windows"))
		}
		// We need a secret to compute the guard cookie even if encryption is disabled.
		if len(k.Key) == 0 {
			errors = append(errors, fmt.Errorf("KCP guard requires a non-empty key"))
		}
	}

	if k.HeaderTimeout < 1 || k.HeaderTimeout > 3600 {
		errors = append(errors, fmt.Errorf("KCP header_timeout must be between 1-3600 seconds"))
	}

	// Limits: allow -1 for unlimited, otherwise validate sane bounds.
	if k.MaxSessions < -1 || k.MaxSessions > 1_000_000 {
		errors = append(errors, fmt.Errorf("KCP max_sessions must be -1 or between 1-1000000"))
	}
	if k.MaxStreamsTotal < -1 || k.MaxStreamsTotal > 10_000_000 {
		errors = append(errors, fmt.Errorf("KCP max_streams_total must be -1 or between 1-10000000"))
	}
	if k.MaxStreamsPerSession < -1 || k.MaxStreamsPerSession > 1_000_000 {
		errors = append(errors, fmt.Errorf("KCP max_streams_per_session must be -1 or between 1-1000000"))
	}

	if k.Smuxbuf < 1024 {
		errors = append(errors, fmt.Errorf("KCP smuxbuf must be >= 1024 bytes"))
	}
	if k.Streambuf < 1024 {
		errors = append(errors, fmt.Errorf("KCP streambuf must be >= 1024 bytes"))
	}

	return errors
}
