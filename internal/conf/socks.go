package conf

import (
	"fmt"
	"net"
)

type SOCKS5 struct {
	Listen_  string       `yaml:"listen"`
	Username string       `yaml:"username"`
	Password string       `yaml:"password"`
	Listen   *net.UDPAddr `yaml:"-"`
}

func (c *SOCKS5) setDefaults() {}
func (c *SOCKS5) validate() []error {
	var errors []error

	addr, err := validateAddr(c.Listen_, true)
	if err != nil {
		errors = append(errors, err)
	}
	c.Listen = addr

	// Security: prevent accidental open-proxy SOCKS on public/LAN addresses.
	// Allow unauthenticated SOCKS ONLY on loopback. If listening on any non-loopback
	// address (including 0.0.0.0 / ::), require username+password.
	//
	// Prevent open-proxy misconfigurations by requiring auth on non-loopback binds.
	// - If only one of username/password is set -> error (misconfig)
	// - If listen is not loopback and username/password are empty -> refuse to run
	if (c.Username == "") != (c.Password == "") {
		errors = append(errors, fmt.Errorf("socks5.username and socks5.password must be set together"))
	}
	if addr != nil {
		isLoopback := addr.IP != nil && addr.IP.IsLoopback()
		if !isLoopback && (c.Username == "" || c.Password == "") {
			errors = append(errors, fmt.Errorf(
				"SOCKS5 listen address '%s' is not loopback; refusing to run without username/password (bind to 127.0.0.1/::1 or set socks5.username and socks5.password)",
				c.Listen_,
			))
		}
	}
	return errors
}
