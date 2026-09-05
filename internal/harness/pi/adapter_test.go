package pi

import (
	"strings"
	"testing"
)

func TestPluginTemplateOwnsSpawnErrors(t *testing.T) {
	t.Parallel()

	if !strings.Contains(piExtensionTemplate, `child.once("error", warnReporting);`) {
		t.Fatal("expected asynchronous child error handling")
	}
}
