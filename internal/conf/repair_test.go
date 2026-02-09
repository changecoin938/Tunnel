package conf

import "testing"

func TestTryRepairConfig_BrokenLogLevelSedBackref(t *testing.T) {
	in := []byte(`role: "client"

log:
\1 "info"

network:
  interface: "ens3"
`)

	out, changed, _ := tryRepairConfig(in)
	if !changed {
		t.Fatalf("expected changed=true")
	}

	got := string(out)
	if want := `  level: "info"`; !containsLine(got, want) {
		t.Fatalf("expected repaired log level line %q, got:\n%s", want, got)
	}
	if containsLine(got, `\1 "info"`) {
		t.Fatalf("expected broken line to be removed, got:\n%s", got)
	}
}

func TestTryRepairConfig_NoChange(t *testing.T) {
	in := []byte(`role: "client"
log:
  level: "warn"
network:
  interface: "ens3"
`)

	out, changed, _ := tryRepairConfig(in)
	if changed {
		t.Fatalf("expected changed=false, got true (out=%q)", string(out))
	}
}

func containsLine(s, needle string) bool {
	// A tiny helper to keep tests readable; avoid regex deps in tests.
	for _, line := range stringsSplitLines(s) {
		if line == needle {
			return true
		}
	}
	return false
}

func stringsSplitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	// last line (even if empty)
	out = append(out, s[start:])
	return out
}
