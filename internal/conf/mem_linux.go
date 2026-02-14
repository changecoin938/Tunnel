//go:build linux

package conf

import (
	"bufio"
	"bytes"
	"os"
	"strconv"
	"strings"
)

func totalMemMB() int {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		// Example: "MemTotal:       16384256 kB"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.Atoi(fields[1])
		if err != nil || kb <= 0 {
			return 0
		}
		return kb / 1024
	}
	return 0
}
