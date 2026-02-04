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
	if (c.Username == "") != (c.Password == "") {
		errors = append(errors, fmt.Errorf("socks5 username/password must both be set (or both be empty)"))
	}
	if c.Listen != nil {
		ip := c.Listen.IP
		isLoopback := ip != nil && ip.IsLoopback()
		if !isLoopback {
			if c.Username == "" || c.Password == "" {
				errors = append(errors, fmt.Errorf("SOCKS5 listen address '%s' is not loopback; refusing to run without username/password (bind to 127.0.0.1/::1 or set socks5.username and socks5.password)", c.Listen_))
			}
		}
	}

	return errors
}
