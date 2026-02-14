//go:build !linux && !darwin && !windows

package conf

func totalMemMB() int { return 0 }
