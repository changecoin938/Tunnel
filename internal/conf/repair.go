package conf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	reLogBlockLine = regexp.MustCompile(`^([[:space:]]*)log:[[:space:]]*(#.*)?$`)

	// Example broken line produced by an incorrect sed replacement:
	//   \1 "info"
	// where "\1" was intended as a backreference to "level:".
	reBrokenSedBackrefLine = regexp.MustCompile(`^[[:space:]]*\\1[[:space:]]+\"?([A-Za-z]+)\"?[[:space:]]*(#.*)?$`)
)

// tryRepairConfig attempts safe, narrow repairs for known config corruptions.
// It returns the (possibly) modified bytes, whether anything changed, and a short reason.
func tryRepairConfig(data []byte) (repaired []byte, changed bool, reason string) {
	out, ok := repairBrokenLogLevelSedBackref(data)
	if ok {
		return out, true, `repaired log.level line ("\1 \"...\")`
	}
	return data, false, ""
}

func repairBrokenLogLevelSedBackref(data []byte) ([]byte, bool) {
	// Work line-by-line to keep diffs minimal and avoid reformatting the whole file.
	lines := strings.Split(string(data), "\n")

	inLog := false
	logIndent := ""
	changed := false

	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}

		// Enter log: block (respect indentation).
		if m := reLogBlockLine.FindStringSubmatch(line); m != nil {
			inLog = true
			logIndent = m[1]
			continue
		}

		if inLog {
			// Exit log block when we hit a less-indented top-level key.
			if !strings.HasPrefix(line, logIndent) {
				inLog = false
			} else {
				// If indentation is <= logIndent (i.e., not inside the block), exit.
				// This also covers the common top-level case where logIndent == "".
				if len(line) > 0 {
					lead := len(line) - len(strings.TrimLeft(line, " \t"))
					base := len(logIndent)
					if lead <= base && strings.Contains(trim, ":") {
						inLog = false
					}
				}
			}
		}

		if !inLog {
			continue
		}

		m := reBrokenSedBackrefLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		level := strings.ToLower(m[1])
		switch level {
		case "none", "debug", "info", "warn", "error", "fatal":
		default:
			continue
		}
		lines[i] = fmt.Sprintf("%s  level: %q", logIndent, level)
		changed = true
	}

	if !changed {
		return data, false
	}
	return []byte(strings.Join(lines, "\n")), true
}

func persistRepairedConfig(path string, original, repaired []byte, reason string) error {
	if path == "" {
		return nil
	}
	if bytes.Equal(original, repaired) {
		return nil
	}

	st, err := os.Stat(path)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	backup := filepath.Join(dir, fmt.Sprintf("%s.bak.auto.%d", base, time.Now().Unix()))

	// Best-effort backup. If this fails we still attempt to write repaired bytes.
	_ = os.WriteFile(backup, original, st.Mode().Perm())

	if err := os.WriteFile(path, repaired, st.Mode().Perm()); err != nil {
		return err
	}

	// Avoid relying on flog here; config loading happens before log level is configured.
	_, _ = fmt.Fprintf(os.Stderr, "paqet: auto-repaired config %s (%s); backup: %s\n", path, reason, backup)
	return nil
}
