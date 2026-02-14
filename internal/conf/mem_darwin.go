//go:build darwin

package conf

import "golang.org/x/sys/unix"

func totalMemMB() int {
	v, err := unix.SysctlUint64("hw.memsize")
	if err != nil || v == 0 {
		return 0
	}
	return int(v / (1024 * 1024))
}
