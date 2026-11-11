package adapter

import (
	"testing"

	"github.com/tomato-hug/homing/internal/driver"
)

func TestCatalogACPSpecsResolveToCatalogEntries(t *testing.T) {
	ids := []string{
		"gemini-cli", "opencode", "cline", "copilot-cli",
		"kiro", "openhands", "goose", "kimi", "cursor",
		"qwen-code", "droid", "qoder", "trae",
	}
	for _, id := range ids {
		if _, ok := catalogSpecByID(id); !ok {
			t.Errorf("catalog entry %q is missing", id)
		}
		spec, ok := driver.SpecFor(id)
		if !ok || !spec.Dispatch || len(spec.Command) == 0 {
			t.Errorf("driver spec %q = %+v, ok=%v", id, spec, ok)
		}
	}
}
