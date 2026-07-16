package agentstate

import (
	"bytes"
	"cmp"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	manifestSchemaVersion = 1
	maxManifestBytes      = 1 << 20
)

var (
	//go:embed manifests/*.toml
	bundledManifests    embed.FS
	errManifestInvalid  = errors.New("invalid detection manifest")
	errInvalidRegion    = errors.New("invalid manifest region")
	errManifestTooLarge = errors.New("detection manifest exceeds 1 MiB")
)

type Manifest struct {
	Version int    `json:"version"           toml:"version"`
	Agent   string `json:"agent"             toml:"agent"`
	Rules   []Rule `json:"rules"             toml:"rules"`
	Source  string `json:"source"            toml:"-"`
	Warning string `json:"warning,omitempty" toml:"-"`
}

type Rule struct {
	ID            string   `json:"id"                        toml:"id"`
	State         string   `json:"state"                     toml:"state"`
	Priority      int      `json:"priority"                  toml:"priority"`
	Region        string   `json:"region"                    toml:"region"`
	CaseSensitive bool     `json:"case_sensitive"            toml:"case_sensitive"`
	All           []string `json:"all,omitempty"             toml:"all"`
	Any           []string `json:"any,omitempty"             toml:"any"`
	None          []string `json:"none,omitempty"            toml:"none"`
	RegexAll      []string `json:"regex_all,omitempty"       toml:"regex_all"`
	RegexAny      []string `json:"regex_any,omitempty"       toml:"regex_any"`
	RegexNone     []string `json:"regex_none,omitempty"      toml:"regex_none"`
	TitleAny      []string `json:"title_any,omitempty"       toml:"title_any"`
	TitleRegexAny []string `json:"title_regex_any,omitempty" toml:"title_regex_any"`
}

type Loader struct{ ConfigDir string }

func DefaultConfigDir() string {
	if value := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); value != "" {
		return filepath.Join(value, "agent-sessions", "detection")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "agent-sessions", "detection")
}

func (l Loader) Load(harness registry.Harness) (Manifest, error) {
	if !SupportsScreen(harness) {
		return Manifest{}, fmt.Errorf("%w: unsupported screen harness %q", errManifestInvalid, harness)
	}
	bundled, err := loadBundled(harness)
	if err != nil {
		return Manifest{}, err
	}
	configDir := l.ConfigDir
	if configDir == "" {
		configDir = DefaultConfigDir()
	}
	if configDir == "" {
		return bundled, nil
	}
	path := filepath.Join(configDir, string(harness)+".toml")
	data, readErr := readManifestFile(path)
	if errors.Is(readErr, os.ErrNotExist) {
		return bundled, nil
	}
	if readErr != nil {
		bundled.Warning = fmt.Sprintf("reading local override %s: %v", path, readErr)
		return bundled, nil
	}
	local, parseErr := ParseManifest(data, harness)
	if parseErr != nil {
		bundled.Warning = fmt.Sprintf("ignoring invalid local override %s: %v", path, parseErr)
		return bundled, nil
	}
	local.Source = path
	return local, nil
}

func readManifestFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if len(data) > maxManifestBytes {
		return nil, errManifestTooLarge
	}
	return data, nil
}

func ParseManifest(data []byte, harness registry.Harness) (Manifest, error) {
	if len(data) > maxManifestBytes {
		return Manifest{}, errManifestTooLarge
	}
	var manifest Manifest
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: parsing TOML: %w", errManifestInvalid, err)
	}
	if manifest.Agent != string(harness) {
		return Manifest{}, fmt.Errorf("%w: agent %q does not match %q", errManifestInvalid, manifest.Agent, harness)
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func loadBundled(harness registry.Harness) (Manifest, error) {
	path := "manifests/" + string(harness) + ".toml"
	data, err := bundledManifests.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("reading bundled manifest %s: %w", path, err)
	}
	manifest, err := ParseManifest(data, harness)
	if err != nil {
		return Manifest{}, fmt.Errorf("loading bundled manifest %s: %w", path, err)
	}
	manifest.Source = "bundled:" + path
	return manifest, nil
}

func validateManifest(manifest Manifest) error {
	if manifest.Version != manifestSchemaVersion || len(manifest.Rules) == 0 {
		return fmt.Errorf("%w: schema version %d or empty rules", errManifestInvalid, manifest.Version)
	}
	seen := make(map[string]struct{}, len(manifest.Rules))
	for index, rule := range manifest.Rules {
		if strings.TrimSpace(rule.ID) == "" {
			return fmt.Errorf("%w: rules[%d] has no id", errManifestInvalid, index)
		}
		if _, exists := seen[rule.ID]; exists {
			return fmt.Errorf("%w: duplicate rule id %q", errManifestInvalid, rule.ID)
		}
		seen[rule.ID] = struct{}{}
		if err := validateRule(rule); err != nil {
			return err
		}
	}
	return nil
}

func validateRule(rule Rule) error {
	activity, err := registry.NormalizeActivity(rule.State)
	if err != nil || activity == registry.ActivityUnknown || activity == "" {
		return fmt.Errorf("%w: rule %q has unsupported state %q", errManifestInvalid, rule.ID, rule.State)
	}
	if _, err := selectRegion(rule.Region, nil); err != nil {
		return fmt.Errorf("%w: rule %q: %w", errManifestInvalid, rule.ID, err)
	}
	if !hasPositiveMatcher(rule) {
		return fmt.Errorf("%w: rule %q has no positive matcher", errManifestInvalid, rule.ID)
	}
	expressions := append([]string{}, rule.RegexAll...)
	expressions = append(expressions, rule.RegexAny...)
	expressions = append(expressions, rule.RegexNone...)
	expressions = append(expressions, rule.TitleRegexAny...)
	for _, expression := range expressions {
		if _, err := regexp.Compile(ruleRegexExpression(expression, rule.CaseSensitive)); err != nil {
			return fmt.Errorf("%w: rule %q regex %q: %w", errManifestInvalid, rule.ID, expression, err)
		}
	}
	return nil
}

func hasPositiveMatcher(rule Rule) bool {
	return len(rule.All)+len(rule.Any)+len(rule.RegexAll)+len(rule.RegexAny)+len(rule.TitleAny)+len(rule.TitleRegexAny) > 0
}

func sortedRules(rules []Rule) []Rule {
	result := append([]Rule(nil), rules...)
	slices.SortStableFunc(result, func(left, right Rule) int { return cmp.Compare(right.Priority, left.Priority) })
	return result
}

func selectRegion(value string, lines []string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "all" {
		return lines, nil
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 || (parts[0] != "bottom" && parts[0] != "top") {
		return nil, fmt.Errorf("%w: %q", errInvalidRegion, value)
	}
	count, err := strconv.Atoi(parts[1])
	if err != nil || count <= 0 {
		return nil, fmt.Errorf("%w: %q", errInvalidRegion, value)
	}
	count = min(count, len(lines))
	if parts[0] == "top" {
		return lines[:count], nil
	}
	return lines[len(lines)-count:], nil
}
