package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const (
	DefaultDir      = ".mnemo"
	DefaultFileName = "config.yaml"
	configType      = "yaml"
)

// Config is the root Mnemo configuration loaded from .mnemo/config.yaml.
type Config struct {
	Database DatabaseConfig `yaml:"database"           mapstructure:"database"`
	Privacy  PrivacyConfig  `yaml:"privacy,omitempty"  mapstructure:"privacy"`
	Tasks    TasksConfig    `yaml:"tasks,omitempty"    mapstructure:"tasks"`
}

// TasksConfig tunes task lifecycle behavior.
type TasksConfig struct {
	// ColdAfter is a duration string (e.g. "336h", "14d" is not valid Go
	// duration — use hours). Empty → DefaultColdAfter.
	ColdAfter string `yaml:"cold_after,omitempty" mapstructure:"cold_after"`
}

// ColdAfterDuration parses Tasks.ColdAfter. It returns 0 when unset or
// invalid, letting the caller fall back to the tasksvc default (decay must
// never be accidentally disabled by a bad config value).
func (c Config) ColdAfterDuration() time.Duration {
	if c.Tasks.ColdAfter == "" {
		return 0
	}
	d, err := time.ParseDuration(c.Tasks.ColdAfter)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

// DatabaseConfig describes the configured storage backend.
type DatabaseConfig struct {
	Type string `yaml:"type" mapstructure:"type"`
	DSN  string `yaml:"dsn"  mapstructure:"dsn"`
}

// PrivacyConfig gates data that may leave the local process. Zero values are
// the safe defaults.
type PrivacyConfig struct {
	// AllowCrossVendorEgress permits injecting a state of play derived from
	// one vendor's agent into a different vendor's agent.
	AllowCrossVendorEgress bool `yaml:"allow_cross_vendor_egress,omitempty" mapstructure:"allow_cross_vendor_egress"`
	// ShareMetadataToTeam opts a repo into the shared/PostgreSQL backend.
	// Even when true, the Postgres adapter stores metadata + compiled
	// working state only — never raw transcript content or absolute paths
	// (enforced in internal/storage/postgres).
	ShareMetadataToTeam bool `yaml:"share_metadata_to_team,omitempty" mapstructure:"share_metadata_to_team"`
}

// Default returns the local-first default configuration.
func Default() Config {
	return Config{
		Database: DatabaseConfig{
			Type: "sqlite",
			DSN:  filepath.Join(DefaultDir, "mnemo.db"),
		},
	}
}

// DefaultPath returns the conventional config path under a repository root.
func DefaultPath(repoRoot string) string {
	return filepath.Join(repoRoot, DefaultDir, DefaultFileName)
}

// Load reads a Mnemo config file from disk.
func Load(path string) (Config, error) {
	v := newViper()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return Config{}, err
	}
	return unmarshal(v)
}

// Save writes a Mnemo config file without overwriting an existing file.
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		return fmt.Errorf("config already exists at %s", path)
	}
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(MarshalYAML(cfg))
	return err
}

// Parse reads a Mnemo config from a YAML reader.
func Parse(reader io.Reader) (Config, error) {
	v := newViper()
	if err := v.ReadConfig(reader); err != nil {
		return Config{}, err
	}
	return unmarshal(v)
}

// MarshalYAML renders a Config as YAML.
func MarshalYAML(cfg Config) string {
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func newViper() *viper.Viper {
	v := viper.New()
	v.SetConfigType(configType)

	def := Default()
	v.SetDefault("database.type", def.Database.Type)
	v.SetDefault("database.dsn", def.Database.DSN)

	return v
}

func unmarshal(v *viper.Viper) (Config, error) {
	var cfg Config
	if err := v.UnmarshalExact(&cfg); err != nil {
		return Config{}, err
	}

	if cfg.Database.Type == "" {
		return Config{}, fmt.Errorf("database.type is required")
	}
	if cfg.Database.DSN == "" {
		return Config{}, fmt.Errorf("database.dsn is required")
	}
	return cfg, nil
}
