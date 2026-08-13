package bridge

import (
	"strings"
	"testing"
)

func TestMergedEnvironmentUsesOverridesOnce(t *testing.T) {
	env := mergedEnvironment(
		[]string{"REG_FACTORY_ROOT=/process/root", "PATH=/bin", "REG_FACTORY_ROOT=/stale/root"},
		map[string]string{"REG_FACTORY_ROOT": "/configured/root"},
		map[string]string{"PYTHONUNBUFFERED": "1"},
	)
	values := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("invalid environment entry %q", entry)
		}
		if _, exists := values[key]; exists {
			t.Fatalf("duplicate environment key %q", key)
		}
		values[key] = value
	}
	if got := values["REG_FACTORY_ROOT"]; got != "/configured/root" {
		t.Fatalf("REG_FACTORY_ROOT=%q", got)
	}
	if got := values["PYTHONUNBUFFERED"]; got != "1" {
		t.Fatalf("PYTHONUNBUFFERED=%q", got)
	}
}
