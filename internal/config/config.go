package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"

	"github.com/zigai/aht/pkg/registry"
)

const (
	// ConfigEnv is the environment variable for overriding the config file path.
	ConfigEnv = "AHT_CONFIG"

	// maxConfigFileSize caps configuration files to 1 MiB.
	maxConfigFileSize = 1024 * 1024

	defaultDirMode  = 0o700
	defaultFileMode = 0o600

	envSplitParts = 2
)

// Sentinel configuration errors.
var (
	ErrMissingUnitSuffix   = errors.New("missing unit suffix")
	ErrInvalidSort         = errors.New("invalid ui.sort")
	ErrInvalidPresence     = errors.New("invalid ui.default_presence")
	ErrInvalidTimeFormat   = errors.New("invalid ui.time_format")
	ErrNegativeAge         = errors.New("retention.max_gone_age must be non-negative")
	ErrNonPositiveInterval = errors.New("tracker.interval must be positive")
	ErrNegativeGracePeriod = errors.New("tracker.grace_period must be non-negative")
	ErrConfigIsDirectory   = errors.New("config path is a directory")
	ErrConfigFileTooLarge  = errors.New("config file exceeds 1 MiB limit")
	ErrConfigNotFound      = errors.New("config file not found")
	ErrParseConfig         = errors.New("failed to parse config file")
	ErrLoadDefaults        = errors.New("failed to load base defaults")
	ErrLoadEnv             = errors.New("failed to load environment overrides")
	ErrUnmarshalConfig     = errors.New("failed to unmarshal configuration")
	ErrAccessConfig        = errors.New("failed to access config file")
	ErrInvalidDuration     = errors.New("invalid duration")
)

type Config struct {
	UI        UIConfig        `json:"ui"        koanf:"ui"        toml:"ui"`
	Retention RetentionConfig `json:"retention" koanf:"retention" toml:"retention"`
	Filter    FilterConfig    `json:"filter"    koanf:"filter"    toml:"filter"`
	Tracker   TrackerConfig   `json:"tracker"   koanf:"tracker"   toml:"tracker"`
	Detection DetectionConfig `json:"detection" koanf:"detection" toml:"detection"`
}

// UIConfig controls terminal and table display defaults.
type UIConfig struct {
	DefaultPresence string `json:"default_presence,omitempty" koanf:"default_presence" toml:"default_presence"`
	Sort            string `json:"sort,omitempty"             koanf:"sort"             toml:"sort"`
	SortDesc        *bool  `json:"sort_desc,omitempty"        koanf:"sort_desc"        toml:"sort_desc"`
	AbsoluteTime    *bool  `json:"absolute_time,omitempty"    koanf:"absolute_time"    toml:"absolute_time"`
	TimeFormat      string `json:"time_format,omitempty"      koanf:"time_format"      toml:"time_format"`
}

// RetentionConfig controls state retention and tombstone cleanup defaults.
type RetentionConfig struct {
	AutoClean  *bool  `json:"auto_clean,omitempty"   koanf:"auto_clean"   toml:"auto_clean"`
	MaxGoneAge string `json:"max_gone_age,omitempty" koanf:"max_gone_age" toml:"max_gone_age"`
}

// FilterConfig controls default session visibility exclusions.
type FilterConfig struct {
	IgnoreHarnesses []string `json:"ignore_harnesses,omitempty" koanf:"ignore_harnesses" toml:"ignore_harnesses"`
	IgnorePaths     []string `json:"ignore_paths,omitempty"     koanf:"ignore_paths"     toml:"ignore_paths"`
}

// TrackerConfig controls background observer behavior.
type TrackerConfig struct {
	Interval    string `json:"interval,omitempty"     koanf:"interval"     toml:"interval"`
	GracePeriod string `json:"grace_period,omitempty" koanf:"grace_period" toml:"grace_period"`
	Quiet       *bool  `json:"quiet,omitempty"        koanf:"quiet"        toml:"quiet"`
}

// DetectionConfig controls agent and screen inspection defaults.
type DetectionConfig struct {
	ManifestsDir     string `json:"manifests_dir,omitempty"     koanf:"manifests_dir"     toml:"manifests_dir"`
	ScreenInspection *bool  `json:"screen_inspection,omitempty" koanf:"screen_inspection" toml:"screen_inspection"`
}

// DefaultPath returns the default path to the user's config file.
func DefaultPath() string {
	if val := strings.TrimSpace(os.Getenv(ConfigEnv)); val != "" {
		return val
	}
	configDir, err := os.UserConfigDir()
	if err == nil && configDir != "" {
		return filepath.Join(configDir, "aht", "config.toml")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "aht", "config.toml")
	}
	return filepath.Join(os.TempDir(), "aht", "config.toml")
}

// DefaultConfigTemplate returns a formatted TOML string containing every default
// option with descriptive comments.
func DefaultConfigTemplate() string {
	return `# AHT Configuration (config.toml)
# Track local coding-agent sessions and where they are running.
# Override location with $AHT_CONFIG or --config <path>.

[ui]
# Default session presence filter: "live", "gone", "unknown", or "all"
default_presence = "all"

# Sort sessions by: "updated", "created", "harness", "presence", "activity", "cwd", "id", "multiplexer", "tmux"
sort = "updated"

# Sort in descending order
sort_desc = false

# Display timestamps in absolute format rather than relative
absolute_time = false

# Timestamp display format: "relative", "absolute", or "iso8601"
time_format = "relative"

[retention]
# Automatically clean up expired gone sessions in background tracker
auto_clean = false

# Maximum age of gone sessions before tombstone cleanup, e.g. "7d", "24h"
max_gone_age = "7d"

[filter]
# List of harnesses to omit from default session listings unless explicitly requested via --agent
ignore_harnesses = []

# List of glob or directory path patterns matching session working directories to ignore
ignore_paths = []

[tracker]
# Reconciliation polling frequency
interval = "300ms"

# Process absence grace period before marking sessions gone
grace_period = "0s"

# Suppress human cycle output and diagnostics from tracker
quiet = false

[detection]
# Directory containing custom agent screen detection manifest TOML files
manifests_dir = ""

# Enable terminal multiplexer screen inspection
screen_inspection = true
`
}

// WriteConfigFile writes the default configuration template to path, creating
// parent directories if needed. It overwrites any existing file at path.
func WriteConfigFile(path string) error {
	if path == "" {
		path = DefaultPath()
	}
	_, err := publishConfigFile(path, true)
	return err
}

// EnsureConfigFile ensures that a configuration file exists at path.
// If path is empty, DefaultPath() is used.
// If the file already exists, it returns created=false, nil.
// If the file does not exist, it creates the parent directories and writes DefaultConfigTemplate()
// with 0o600 permissions, returning created=true, nil. Publication is atomic and
// never overwrites a file created by another process.
func EnsureConfigFile(path string) (bool, error) {
	if path == "" {
		path = DefaultPath()
	}
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return false, fmt.Errorf("%w: %s", ErrConfigIsDirectory, path)
		}
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("%w %s: %w", ErrAccessConfig, path, err)
	}

	return publishConfigFile(path, false)
}

// ParseDuration parses duration strings including day suffixes (e.g. "7d", "24h", "10s").
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") || strings.HasSuffix(s, "D") {
		valStr := strings.TrimSpace(s[:len(s)-1])
		days, err := strconv.ParseInt(valStr, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q: %w", s, err)
		}
		const day = 24 * time.Hour
		if days > math.MaxInt64/int64(day) || days < math.MinInt64/int64(day) {
			return 0, fmt.Errorf("%w: day duration %q is out of range", ErrInvalidDuration, s)
		}
		return time.Duration(days) * day, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		if _, numErr := strconv.Atoi(s); numErr == nil {
			return 0, fmt.Errorf("%w: %q (e.g. %q or %q)", ErrMissingUnitSuffix, s, s+"d", s+"h")
		}
		return 0, fmt.Errorf("%w: %w", ErrInvalidDuration, err)
	}
	return d, nil
}

var validSortKeys = map[string]string{
	"updated":             "updated",
	"time":                "updated",
	"updated-at":          "updated",
	"created":             "created",
	"harness":             "harness",
	"agent":               "harness",
	"presence":            "presence",
	"activity":            "activity",
	"cwd":                 "cwd",
	"id":                  "id",
	"multiplexer":         "multiplexer",
	"mux":                 "multiplexer",
	"tmux":                "tmux",
	"presence-changed":    "presence-changed",
	"presence-changed-at": "presence-changed",
	"presence-since":      "presence-changed",
	"activity-changed":    "activity-changed",
	"activity-changed-at": "activity-changed",
	"activity-since":      "activity-changed",
}

// NormalizeSort validates and normalizes sort key names.
func NormalizeSort(s string) (string, error) {
	norm := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", "-"))
	if key, ok := validSortKeys[norm]; ok {
		return key, nil
	}
	return "", fmt.Errorf("%w %q, must be one of: updated, created, harness, presence, activity, cwd, id, multiplexer, tmux, presence-changed, activity-changed", ErrInvalidSort, s)
}

// Validate checks configuration invariants.
func (c Config) Validate() error {
	if err := c.validateUI(); err != nil {
		return err
	}
	if err := c.validateRetention(); err != nil {
		return err
	}
	return c.validateTracker()
}

func (c Config) validateUI() error {
	if c.UI.DefaultPresence != "" {
		trimmed := strings.ToLower(strings.TrimSpace(c.UI.DefaultPresence))
		if trimmed != "all" {
			if _, err := registry.NormalizePresence(trimmed); err != nil {
				return fmt.Errorf("%w %q (allowed: live, gone, unknown, all): %w", ErrInvalidPresence, c.UI.DefaultPresence, err)
			}
		}
	}
	if c.UI.Sort != "" {
		if _, err := NormalizeSort(c.UI.Sort); err != nil {
			return err
		}
	}
	if c.UI.TimeFormat != "" {
		tf := strings.ToLower(strings.TrimSpace(c.UI.TimeFormat))
		if tf != "relative" && tf != "absolute" && tf != "iso8601" {
			return fmt.Errorf("%w %q (allowed: relative, absolute, iso8601)", ErrInvalidTimeFormat, c.UI.TimeFormat)
		}
	}
	return nil
}

func (c Config) validateRetention() error {
	if c.Retention.MaxGoneAge != "" {
		d, err := ParseDuration(c.Retention.MaxGoneAge)
		if err != nil {
			return fmt.Errorf("invalid retention.max_gone_age %q: %w", c.Retention.MaxGoneAge, err)
		}
		if d < 0 {
			return ErrNegativeAge
		}
	}
	return nil
}

func (c Config) validateTracker() error {
	if c.Tracker.Interval != "" {
		d, err := ParseDuration(c.Tracker.Interval)
		if err != nil {
			return fmt.Errorf("invalid tracker.interval %q: %w", c.Tracker.Interval, err)
		}
		if d <= 0 {
			return ErrNonPositiveInterval
		}
	}
	if c.Tracker.GracePeriod != "" {
		d, err := ParseDuration(c.Tracker.GracePeriod)
		if err != nil {
			return fmt.Errorf("invalid tracker.grace_period %q: %w", c.Tracker.GracePeriod, err)
		}
		if d < 0 {
			return ErrNegativeGracePeriod
		}
	}
	return nil
}

// Load loads, parses, and validates the configuration file from path.
// If path is empty, DefaultPath() is used.
// A missing default config file is silently skipped; a missing explicitly specified path is an error.
func Load(path string) (Config, string, error) {
	explicit := path != ""
	resolvedPath := path
	if !explicit {
		resolvedPath = DefaultPath()
	}

	k := koanf.New(".")

	// Layer 1: Base defaults
	if err := k.Load(structs.Provider(defaultConfig(), "koanf"), nil); err != nil {
		return Config{}, resolvedPath, fmt.Errorf("%w: %w", ErrLoadDefaults, err)
	}

	// Layer 2: TOML file
	info, err := os.Stat(resolvedPath)
	switch {
	case err == nil:
		if info.IsDir() {
			return Config{}, resolvedPath, fmt.Errorf("%w: %s", ErrConfigIsDirectory, resolvedPath)
		}
		if info.Size() > maxConfigFileSize {
			return Config{}, resolvedPath, fmt.Errorf("%w (%d bytes): %s", ErrConfigFileTooLarge, info.Size(), resolvedPath)
		}
		if err := k.Load(boundedConfigFile(resolvedPath), toml.Parser()); err != nil {
			return Config{}, resolvedPath, fmt.Errorf("%w %s: %w", ErrParseConfig, resolvedPath, err)
		}
	case explicit && errors.Is(err, os.ErrNotExist):
		return Config{}, resolvedPath, fmt.Errorf("%w %s: %w", ErrConfigNotFound, resolvedPath, err)
	case !errors.Is(err, os.ErrNotExist):
		return Config{}, resolvedPath, fmt.Errorf("%w %s: %w", ErrAccessConfig, resolvedPath, err)
	}

	// Layer 3: Environment overrides
	if err := k.Load(env.Provider(".", env.Opt{
		Prefix:        "AHT_",
		TransformFunc: envTransform,
	}), nil); err != nil {
		return Config{}, resolvedPath, fmt.Errorf("%w: %w", ErrLoadEnv, err)
	}

	// Layer 4: Unmarshal & Validate
	var cfg Config
	decoderConfig := &mapstructure.DecoderConfig{
		ErrorUnused:      true,
		WeaklyTypedInput: true,
		TagName:          "koanf",
		Result:           &cfg,
	}
	if err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{
		Tag:           "koanf",
		DecoderConfig: decoderConfig,
	}); err != nil {
		return Config{}, resolvedPath, fmt.Errorf("%w: %w", ErrUnmarshalConfig, err)
	}

	cfg, err = normalizeConfig(cfg)
	return cfg, resolvedPath, err
}

func normalizeConfig(cfg Config) (Config, error) {
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	cfg.UI.DefaultPresence = strings.ToLower(strings.TrimSpace(cfg.UI.DefaultPresence))
	cfg.UI.TimeFormat = strings.ToLower(strings.TrimSpace(cfg.UI.TimeFormat))
	if cfg.UI.Sort != "" {
		var err error
		cfg.UI.Sort, err = NormalizeSort(cfg.UI.Sort)
		if err != nil {
			return Config{}, err
		}
	}

	return cfg, nil
}

func defaultConfig() Config {
	return Config{}
}

func envTransform(k, v string) (string, any) {
	trimmed := strings.TrimPrefix(k, "AHT_")
	parts := strings.SplitN(trimmed, "_", envSplitParts)
	section := strings.ToLower(parts[0])

	switch section {
	case "ui", "retention", "filter", "tracker", "detection":
	default:
		return "", nil
	}

	if len(parts) < envSplitParts {
		return section, v
	}

	field := strings.ToLower(parts[1])
	key := section + "." + field

	if key == "filter.ignore_harnesses" || key == "filter.ignore_paths" {
		if strings.TrimSpace(v) == "" {
			return key, []string{}
		}
		items := strings.Split(v, ",")
		var clean []string
		for _, item := range items {
			if t := strings.TrimSpace(item); t != "" {
				clean = append(clean, t)
			}
		}
		return key, clean
	}

	return key, v
}
