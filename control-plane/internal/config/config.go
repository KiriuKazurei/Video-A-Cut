// Package config loads the control plane's runtime settings from a file. It
// validates every value once at startup so a bad setting fails the process
// before it can serve a wrong answer.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"time"
)

// Sentinels. Callers must identify them with errors.Is.
var (
	// ErrInvalid wraps every out-of-range value found by Validate. It exists
	// so a caller can tell "this file has a value I do not trust" from
	// "this file could not be read at all" without string matching.
	ErrInvalid = errors.New("config: invalid value")
)

// Defaults and bounds. They are constants rather than fields on Config
// because Default() has to return them and Validate has to reject values
// outside them, and a single definition cannot drift between the two.
const (
	// DefaultLeaseSeconds is how long a claimed task stays leased before the
	// reclaimer may hand it back.
	DefaultLeaseSeconds = 30
	// DefaultMaxAgents is how many agents may hold a lease at once.
	DefaultMaxAgents = 2
	// DefaultAuditRetentionDays is how long normal audit rows are kept.
	DefaultAuditRetentionDays = 30
	// DefaultArchiveRetentionDays is how long archived rows are kept.
	DefaultArchiveRetentionDays = 60
)

// Bounds on lease duration. A lease longer than maxLeaseSeconds would keep a
// task stranded for an hour before the reclaimer looks at it, which is long
// enough that nobody would notice it stuck.
const (
	maxLeaseSeconds = 3600
	maxAgents       = 64
)

// Config is the whole runtime configuration of the control plane. Every field
// is validated exactly once, by Validate, and every consumer can therefore
// assume a Config it received is trustworthy.
type Config struct {
	// LeaseSeconds is how long a claimed task stays leased before the
	// reclaimer may hand it back. Default 30.
	LeaseSeconds int
	// MaxAgents is how many agents may hold a lease at once. Default 2,
	// raised only by hand in this file.
	MaxAgents int
	// AuditRetentionDays is how long normal audit rows are kept before the
	// sweeper removes them. Default 30.
	AuditRetentionDays int
	// ArchiveRetentionDays is how long archived rows are kept before the
	// sweeper removes them. Default 60.
	ArchiveRetentionDays int
}

// Default returns the configuration the process runs with when no file is
// present: the documented deployment is a single file whose absence means
// "run with defaults".
func Default() Config {
	return Config{
		LeaseSeconds:         DefaultLeaseSeconds,
		MaxAgents:            DefaultMaxAgents,
		AuditRetentionDays:   DefaultAuditRetentionDays,
		ArchiveRetentionDays: DefaultArchiveRetentionDays,
	}
}

// Load reads the JSON config file at path and returns the result of applying
// it over Default(), validated.
//
// The file format is JSON with snake_case keys: lease_seconds, max_agents,
// audit_retention_days, archive_retention_days. lease_seconds also accepts a
// duration string ("30s", "5m") so an operator never has to convert units in
// their head.
//
// A missing file is not an error. The documented deployment is a single file
// whose absence means "run with defaults", and treating it as a failure would
// make the normal first boot look like a broken installation.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default(), nil
		}
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}

	cfg := Default()
	if err := apply(&cfg, doc); err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, nil
}

// Validate reports the first setting whose value the process refuses to run
// with. Each violation wraps ErrInvalid and names both the field and the
// offending value, so a log line is enough to find the line to fix.
func (c Config) Validate() error {
	if c.LeaseSeconds <= 0 || c.LeaseSeconds > maxLeaseSeconds {
		return fmt.Errorf("config: lease_seconds must be > 0 and <= %d: got %d: %w", maxLeaseSeconds, c.LeaseSeconds, ErrInvalid)
	}
	if c.MaxAgents <= 0 || c.MaxAgents > maxAgents {
		return fmt.Errorf("config: max_agents must be > 0 and <= %d: got %d: %w", maxAgents, c.MaxAgents, ErrInvalid)
	}
	if c.AuditRetentionDays <= 0 {
		return fmt.Errorf("config: audit_retention_days must be > 0: got %d: %w", c.AuditRetentionDays, ErrInvalid)
	}
	if c.ArchiveRetentionDays <= 0 {
		return fmt.Errorf("config: archive_retention_days must be > 0: got %d: %w", c.ArchiveRetentionDays, ErrInvalid)
	}
	return nil
}

// apply decodes the recognised keys of raw over cfg, in place.
//
// Unknown keys are ignored rather than rejected: a config file written for a
// newer build must still boot on this one, and the alternative is a process
// that refuses to start because a peer added a setting.
//
// A recognised key with a value of the wrong type is an error naming the key.
// Silently keeping the default would hide a typo in the file's brackets or a
// quoted number, which is exactly the class of mistake the file is there to
// make impossible.
func apply(cfg *Config, raw map[string]any) error {
	if v, ok := raw["lease_seconds"]; ok {
		n, err := decodeLeaseSeconds(v)
		if err != nil {
			return fmt.Errorf("lease_seconds: %w", err)
		}
		cfg.LeaseSeconds = n
	}
	if v, ok := raw["max_agents"]; ok {
		n, err := decodeInt(v)
		if err != nil {
			return fmt.Errorf("max_agents: %w", err)
		}
		cfg.MaxAgents = n
	}
	if v, ok := raw["audit_retention_days"]; ok {
		n, err := decodeInt(v)
		if err != nil {
			return fmt.Errorf("audit_retention_days: %w", err)
		}
		cfg.AuditRetentionDays = n
	}
	if v, ok := raw["archive_retention_days"]; ok {
		n, err := decodeInt(v)
		if err != nil {
			return fmt.Errorf("archive_retention_days: %w", err)
		}
		cfg.ArchiveRetentionDays = n
	}
	return nil
}

// decodeInt turns a JSON-decoded number into an int. json.Unmarshal yields
// float64 for every number, so that is the case that matters here; the int and
// int64 arms exist for callers building the map by hand.
func decodeInt(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		return floatToInt(n)
	case float32:
		return floatToInt(float64(n))
	case int:
		return n, nil
	case int64:
		return int(n), nil
	default:
		return 0, fmt.Errorf("expected an integer, got %s (%T)", describeType(v), v)
	}
}

// decodeLeaseSeconds accepts a plain integer count of seconds ("45") or a Go
// duration string ("45s", "5m"). An operator writing "5m" should not get a
// cryptic parse failure; a duration that does not resolve to a whole second
// ("1500ms") is rejected because a fractional lease makes no sense.
func decodeLeaseSeconds(v any) (int, error) {
	switch s := v.(type) {
	case string:
		if d, err := time.ParseDuration(s); err == nil {
			if d != d.Truncate(time.Second) {
				return 0, fmt.Errorf("expected a whole number of seconds, got %q", s)
			}
			return int(d / time.Second), nil
		}
		if n, err := strconv.Atoi(s); err == nil {
			return n, nil
		}
		return 0, fmt.Errorf("expected seconds or a duration such as \"30s\"/\"5m\", got %q", s)
	case float64:
		return floatToInt(s)
	case float32:
		return floatToInt(float64(s))
	case int:
		return s, nil
	case int64:
		return int(s), nil
	default:
		return 0, fmt.Errorf("expected seconds or a duration such as \"30s\"/\"5m\", got %s (%T)", describeType(v), v)
	}
}

// floatToInt rejects a number that is not a whole integer, so "1.5" agents
// cannot pass through an int field as 1.
func floatToInt(f float64) (int, error) {
	if f != float64(int(f)) {
		return 0, fmt.Errorf("expected a whole number, got %v", f)
	}
	return int(f), nil
}

// describeType names a decoded JSON value for an error message without
// exposing Go's type names to an operator editing a config file.
func describeType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "a boolean"
	case string:
		return "a string"
	case []any:
		return "a list"
	case map[string]any:
		return "an object"
	default:
		return fmt.Sprintf("%T", v)
	}
}
