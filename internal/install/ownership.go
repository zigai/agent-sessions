package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	harnesspkg "github.com/zigai/agent-sessions/v2/pkg/harness"
)

// ArtifactStatus describes whether a managed integration artifact is absent,
// up to date, an older managed generation, or owned by somebody else.
type ArtifactStatus string

const (
	ArtifactMissing ArtifactStatus = "missing"
	ArtifactCurrent ArtifactStatus = "current"
	ArtifactStale   ArtifactStatus = "stale"
	ArtifactForeign ArtifactStatus = "foreign"
)

const (
	managedIntegrationVersion = harnesspkg.IntegrationVersion
	integrationCaptureGroups  = 2
)

var (
	integrationVersionPattern = regexp.MustCompile(`(?i)(?:agent[_-]?sessions[_-]?integration[_-]?version|AGENT_SESSIONS_INTEGRATION_VERSION)\s*[=:]\s*["']?([0-9]+)`)
	integrationSourcePattern  = regexp.MustCompile(`(?i)agent[_-]?sessions[_-]?integration\s*[=:]`)
	integrationIDPattern      = regexp.MustCompile(`(?i)AGENT_SESSIONS_INTEGRATION_ID\s*=\s*["']?([a-z0-9_-]+)`)
)

// ClassifyArtifact inspects a generated artifact without modifying it. It is
// intentionally format-agnostic: managed ownership is established by the
// marker or source metadata and generation is established by the version.
func ClassifyArtifact(path string) (ArtifactStatus, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ArtifactMissing, nil
		}
		return "", fmt.Errorf("checking artifact %s: %w", path, err)
	}
	if info.IsDir() {
		data, err := os.ReadFile(filepath.Join(path, ".agent-sessions-managed"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return ArtifactForeign, nil
			}
			return "", fmt.Errorf("reading artifact marker %s: %w", path, err)
		}
		return classifyArtifactContent(string(data)), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading artifact %s: %w", path, err)
	}

	return classifyArtifactContent(string(data)), nil
}

func classifyArtifactContent(content string) ArtifactStatus {
	if !strings.Contains(content, managedMarker) && !integrationSourcePattern.MatchString(content) {
		return ArtifactForeign
	}

	match := integrationVersionPattern.FindStringSubmatch(content)
	if len(match) != integrationCaptureGroups {
		return ArtifactStale
	}
	version, err := strconv.Atoi(match[1])
	if err != nil || version != expectedIntegrationVersion(content) {
		return ArtifactStale
	}

	return ArtifactCurrent
}

func expectedIntegrationVersion(content string) int {
	match := integrationIDPattern.FindStringSubmatch(content)
	if len(match) != integrationCaptureGroups {
		return managedIntegrationVersion
	}
	id, err := harnesspkg.Normalize(match[1])
	if err != nil {
		return managedIntegrationVersion
	}
	return harnesspkg.IntegrationVersionFor(id)
}
