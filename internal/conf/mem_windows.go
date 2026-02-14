//go:build windows

package conf

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func totalMemMB() int {
	var m windows.MemStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	if err := windows.GlobalMemoryStatusEx(&m); err != nil {
		return 0
	}
	if m.TotalPhys == 0 {
		return 0
	}
	return int(m.TotalPhys / (1024 * 1024))
}
