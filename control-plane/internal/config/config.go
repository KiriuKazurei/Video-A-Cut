// Package config loads the control plane's runtime settings from a file. It
// validates every value once at startup so a bad setting fails the process
// before it can serve a wrong answer.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
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
	// DefaultMaxAttempts is how many lease recoveries a task survives.
	DefaultMaxAttempts = 3
	// DefaultAuditRetentionDays is how long normal audit rows are kept.
	DefaultAuditRetentionDays = 30
	// DefaultArchiveRetentionDays is how long archived rows are kept. It
	// matches the normal retention window, so an archived record lives about
	// twice as long as an ordinary one rather than being kept forever.
	DefaultArchiveRetentionDays = 30
	// DefaultHTTPAddr is where the governance surface listens. The plane has
	// no authentication layer yet, so the default binds the loopback
	// interface only: a governance surface that can write asset visibility,
	// locks and approvals must not be reachable from another host until
	// Phase 3 gives it an identity to check. Validation rejects non-loopback
	// bindings while this surface has no authentication.
	DefaultHTTPAddr = "127.0.0.1:8787"
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
	// sweeper removes them. Default 30, so an archived record survives about
	// twice as long as an ordinary one.
	ArchiveRetentionDays int
	// HttpAddr is the host:port the governance HTTP surface binds. Default
	// 127.0.0.1:8787. The empty string disables the surface entirely: the
	// process then runs the workers only, which is the documented headless
	// deployment where nothing on this host serves the WebUI.
	HttpAddr string
	// DeliveryRoot confines human preview and package downloads. Empty disables
	// file delivery until an operator explicitly chooses a directory.
	DeliveryRoot string
	// WebRoot optionally serves the built React application from the same
	// loopback origin as the API. Empty leaves frontend hosting to a proxy.
	WebRoot string
	// MCPAgentsFile enables the agent-only MCP endpoint when set. It contains
	// SHA-256 token hashes, never bearer tokens.
	MCPAgentsFile string
	// MaxAttempts is how many lease recoveries a task survives before the
	// sweep fails it. Default 3.
	MaxAttempts int
	// IngestRoots maps stable root ids to directories recordings may be
	// imported from. Nil disables the raw-recording entry. It is a pointer
	// so Config stays comparable.
	IngestRoots *[]ingest.Root
	// IngestPolicy optionally lowers the default media-ingest budget.
	IngestPolicy *ingest.Policy
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
		HttpAddr:             DefaultHTTPAddr,
		MaxAttempts:          DefaultMaxAttempts,
	}
}

// Load reads the JSON config file at path and returns the result of applying
// it over Default(), validated.
//
// The file format is JSON with snake_case keys: lease_seconds, max_agents,
// audit_retention_days, archive_retention_days, http_addr. lease_seconds also
// accepts a duration string ("30s", "5m") so an operator never has to convert
// units in their head.
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
	// 0 means "use the service default" so hand-built configs stay valid.
	if c.MaxAttempts < 0 || c.MaxAttempts > 100 {
		return fmt.Errorf("config: max_attempts must be 0 (default) or 1..100: got %d: %w", c.MaxAttempts, ErrInvalid)
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
	// An empty address is the documented way to disable the surface, so it
	// is checked for emptiness rather than validity — there is nothing to
	// validate about not listening.
	if c.HttpAddr != "" && !validHTTPAddr(c.HttpAddr) {
		return fmt.Errorf("config: http_addr must be a loopback host:port, got %q: %w", c.HttpAddr, ErrInvalid)
	}
	if c.DeliveryRoot != "" && !filepath.IsAbs(c.DeliveryRoot) {
		return fmt.Errorf("config: delivery_root must be an absolute directory: %w", ErrInvalid)
	}
	if c.WebRoot != "" && !filepath.IsAbs(c.WebRoot) {
		return fmt.Errorf("config: web_root must be an absolute directory: %w", ErrInvalid)
	}
	if c.MCPAgentsFile != "" && (!filepath.IsAbs(c.MCPAgentsFile) || c.HttpAddr == "") {
		return fmt.Errorf("config: mcp_agents_file needs an absolute path and enabled http_addr: %w", ErrInvalid)
	}
	if len(c.Roots()) > 0 && c.DeliveryRoot == "" {
		return fmt.Errorf("config: ingest_roots need delivery_root for owned snapshots: %w", ErrInvalid)
	}
	if err := ingest.ValidateRoots(c.Roots()); err != nil {
		return fmt.Errorf("config: ingest_roots: %v: %w", err, ErrInvalid)
	}
	if c.IngestPolicy != nil {
		if err := c.IngestPolicy.Validate(); err != nil {
			return fmt.Errorf("config: ingest_policy: %v: %w", err, ErrInvalid)
		}
	}
	return nil
}

// validHTTPAddr accepts only a loopback host because the HTTP surface has no
// authentication layer yet. SplitHostPort validates the address shape, and
// the port is checked separately so malformed values fail during config
// validation rather than later in net.Listen.
func validHTTPAddr(addr string) bool {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil || host == "" || portText == "" {
		return false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
	if v, ok := raw["max_attempts"]; ok {
		n, err := decodeInt(v)
		if err != nil {
			return fmt.Errorf("max_attempts: %w", err)
		}
		cfg.MaxAttempts = n
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
	if v, ok := raw["http_addr"]; ok {
		s, err := decodeAddr(v)
		if err != nil {
			return fmt.Errorf("http_addr: %w", err)
		}
		cfg.HttpAddr = s
	}
	if v, ok := raw["delivery_root"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("delivery_root: expected a path string: %w", ErrInvalid)
		}
		cfg.DeliveryRoot = s
	}
	if v, ok := raw["web_root"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("web_root: expected a path string: %w", ErrInvalid)
		}
		cfg.WebRoot = s
	}
	if v, ok := raw["mcp_agents_file"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("mcp_agents_file: expected a path string: %w", ErrInvalid)
		}
		cfg.MCPAgentsFile = s
	}
	if v, ok := raw["ingest_roots"]; ok {
		roots, err := decodeRoots(v)
		if err != nil {
			return fmt.Errorf("ingest_roots: %w", err)
		}
		cfg.IngestRoots = &roots
	}
	if v, ok := raw["ingest_policy"]; ok {
		body, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("ingest_policy: %w", ErrInvalid)
		}
		p := ingest.DefaultPolicy()
		dec := json.NewDecoder(strings.NewReader(string(body)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("ingest_policy: expected a policy object with known fields: %w", ErrInvalid)
		}
		cfg.IngestPolicy = &p
	}
	return nil
}

// Roots returns the configured ingest roots, or nil.
func (c Config) Roots() []ingest.Root {
	if c.IngestRoots == nil {
		return nil
	}
	return *c.IngestRoots
}

// decodeRoots reads [{"root_id","name","path"}]. Paths stay in memory only.
func decodeRoots(v any) ([]ingest.Root, error) {
	list, ok := v.([]any)
	if !ok || len(list) > 32 {
		return nil, fmt.Errorf("expected a list of at most 32 roots: %w", ErrInvalid)
	}
	out := make([]ingest.Root, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok || len(m) != 3 {
			return nil, fmt.Errorf("each root needs exactly root_id, name and path: %w", ErrInvalid)
		}
		id, ok1 := m["root_id"].(string)
		name, ok2 := m["name"].(string)
		path, ok3 := m["path"].(string)
		if !ok1 || !ok2 || !ok3 {
			return nil, fmt.Errorf("root_id, name and path must be strings: %w", ErrInvalid)
		}
		out = append(out, ingest.Root{ID: id, Name: name, Path: path})
	}
	return out, nil
}

// decodeAddr turns a JSON-decoded value into a listen address. Only a string
// is accepted: every other JSON type in this position is an operator writing
// a number where an address belongs, and the default must not be silently
// kept for the same reason decodeInt refuses to.
//
// An empty string decodes to the empty string rather than the default,
// because the empty string is the documented way to disable the surface.
// Substituting the default here would make it impossible to switch the HTTP
// surface off from a file — the key would have to be removed entirely, and a
// file that is dropped into place by a deployment tool would have to know
// that.
func decodeAddr(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("expected a host:port string, got %s (%T)", describeType(v), v)
	}
	return s, nil
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
