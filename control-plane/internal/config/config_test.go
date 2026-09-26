package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes doc to a file inside dir and returns its path.
func writeConfig(t *testing.T, dir, name, doc string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestDefaultValues(t *testing.T) {
	cfg := Default()
	if cfg.LeaseSeconds != 30 {
		t.Errorf("LeaseSeconds = %d, want DefaultLeaseSeconds %d", cfg.LeaseSeconds, DefaultLeaseSeconds)
	}
	if cfg.MaxAgents != 2 {
		t.Errorf("MaxAgents = %d, want DefaultMaxAgents %d", cfg.MaxAgents, DefaultMaxAgents)
	}
	if cfg.AuditRetentionDays != 30 {
		t.Errorf("AuditRetentionDays = %d, want DefaultAuditRetentionDays %d", cfg.AuditRetentionDays, DefaultAuditRetentionDays)
	}
	if cfg.ArchiveRetentionDays != 60 {
		t.Errorf("ArchiveRetentionDays = %d, want DefaultArchiveRetentionDays %d", cfg.ArchiveRetentionDays, DefaultArchiveRetentionDays)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() on Default() = %v, want nil", err)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg != Default() {
		t.Errorf("Load() = %+v, want Default() %+v", cfg, Default())
	}
}

func TestLoadAppliesOverrides(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "control.json", `{
		"lease_seconds": 45,
		"max_agents": 4,
		"audit_retention_days": 7,
		"archive_retention_days": 14
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Config{
		LeaseSeconds:         45,
		MaxAgents:            4,
		AuditRetentionDays:   7,
		ArchiveRetentionDays: 14,
	}
	if cfg != want {
		t.Errorf("Load() = %+v, want %+v", cfg, want)
	}
}

func TestLoadAppliesPartialOverrides(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "control.json", `{"max_agents": 3}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MaxAgents != 3 {
		t.Errorf("MaxAgents = %d, want 3", cfg.MaxAgents)
	}
	if cfg.LeaseSeconds != DefaultLeaseSeconds {
		t.Errorf("LeaseSeconds = %d, want default %d", cfg.LeaseSeconds, DefaultLeaseSeconds)
	}
	if cfg.AuditRetentionDays != DefaultAuditRetentionDays {
		t.Errorf("AuditRetentionDays = %d, want default %d", cfg.AuditRetentionDays, DefaultAuditRetentionDays)
	}
	if cfg.ArchiveRetentionDays != DefaultArchiveRetentionDays {
		t.Errorf("ArchiveRetentionDays = %d, want default %d", cfg.ArchiveRetentionDays, DefaultArchiveRetentionDays)
	}
}

func TestLoadAcceptsDurationStrings(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{`"5m"`, 300},
		{`"30s"`, 30},
		{`"1h"`, 3600},
		{`"90s"`, 90},
		{`"1500"`, 1500},
	} {
		dir := t.TempDir()
		path := writeConfig(t, dir, "control.json", `{"lease_seconds": `+tc.raw+`}`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load(%s) error = %v", tc.raw, err)
		}
		if cfg.LeaseSeconds != tc.want {
			t.Errorf("Load(%s) LeaseSeconds = %d, want %d", tc.raw, cfg.LeaseSeconds, tc.want)
		}
	}
}

func TestUnknownKeysAreIgnored(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "control.json",
		`{"lease_seconds": 45, "some_future_key": 1, "another_future": {"a": 1}}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LeaseSeconds != 45 {
		t.Errorf("LeaseSeconds = %d, want 45", cfg.LeaseSeconds)
	}
	if cfg.MaxAgents != DefaultMaxAgents {
		t.Errorf("MaxAgents = %d, want default %d", cfg.MaxAgents, DefaultMaxAgents)
	}
	if cfg.AuditRetentionDays != DefaultAuditRetentionDays {
		t.Errorf("AuditRetentionDays = %d, want default %d", cfg.AuditRetentionDays, DefaultAuditRetentionDays)
	}
	if cfg.ArchiveRetentionDays != DefaultArchiveRetentionDays {
		t.Errorf("ArchiveRetentionDays = %d, want default %d", cfg.ArchiveRetentionDays, DefaultArchiveRetentionDays)
	}
}

func TestValidateRejectsOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"zero lease", func(c *Config) { c.LeaseSeconds = 0 }},
		{"negative lease", func(c *Config) { c.LeaseSeconds = -1 }},
		{"overnight lease", func(c *Config) { c.LeaseSeconds = 3601 }},
		{"zero agents", func(c *Config) { c.MaxAgents = 0 }},
		{"negative agents", func(c *Config) { c.MaxAgents = -3 }},
		{"overnight agents", func(c *Config) { c.MaxAgents = 65 }},
		{"zero audit retention", func(c *Config) { c.AuditRetentionDays = 0 }},
		{"negative audit retention", func(c *Config) { c.AuditRetentionDays = -7 }},
		{"zero archive retention", func(c *Config) { c.ArchiveRetentionDays = 0 }},
		{"negative archive retention", func(c *Config) { c.ArchiveRetentionDays = -30 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mutate(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want error wrapping ErrInvalid")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("Validate() error = %v, want it to wrap ErrInvalid", err)
			}
			if msg := err.Error(); strings.Contains(msg, "%!") {
				t.Errorf("Validate() error message malformed: %s", msg)
			}
		})
	}
}

func TestLoadRejectsWrongType(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		want string
	}{
		{"lease is a list", `{"lease_seconds": [1, 2]}`, "lease_seconds"},
		{"lease is a boolean", `{"lease_seconds": true}`, "lease_seconds"},
		{"lease is a fraction", `{"lease_seconds": 1.5}`, "lease_seconds"},
		{"lease is a bare word", `{"lease_seconds": "soon"}`, "lease_seconds"},
		{"agents is a string", `{"max_agents": "many"}`, "max_agents"},
		{"audit retention is a boolean", `{"audit_retention_days": false}`, "audit_retention_days"},
		{"archive retention is a list", `{"archive_retention_days": [1]}`, "archive_retention_days"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeConfig(t, dir, "control.json", tc.doc)
			cfg, err := Load(path)
			if err == nil {
				t.Fatalf("Load() = %+v, want error", cfg)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Load() error = %v, want it to name key %q", err, tc.want)
			}
			if cfg != (Config{}) {
				t.Errorf("Load() returned cfg = %+v on error, want zero Config", cfg)
			}
		})
	}
}

func TestLoadRejectsOutOfRangeFromFile(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "control.json", `{"max_agents": 999}`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() = nil error, want out-of-range rejection")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("Load() error = %v, want it to wrap ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "max_agents") {
		t.Errorf("Load() error = %v, want it to name max_agents", err)
	}
}

func TestValidateAcceptsBoundaries(t *testing.T) {
	cfg := Config{
		LeaseSeconds:         maxLeaseSeconds,
		MaxAgents:            1,
		AuditRetentionDays:   1,
		ArchiveRetentionDays: DefaultArchiveRetentionDays,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}
