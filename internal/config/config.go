package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const (
	DefaultDir      = ".mnemo"
	DefaultFileName = "config.yaml"
	configType      = "yaml"
)

// Config is the effective Mnemo configuration. It is built by layering the
// machine-level global config under the per-project .mnemo/config.yaml (see
// LoadLayered). database is machine-level; agents/contexts/privacy/tasks are
// per-project.
type Config struct {
	Database   DatabaseConfig   `yaml:"database"             mapstructure:"database"`
	Privacy    PrivacyConfig    `yaml:"privacy,omitempty"    mapstructure:"privacy"`
	Tasks      TasksConfig      `yaml:"tasks,omitempty"      mapstructure:"tasks"`
	Enrichment EnrichmentConfig `yaml:"enrichment,omitempty" mapstructure:"enrichment"`
	Agents     []AgentConfig    `yaml:"agents,omitempty"     mapstructure:"agents"`
	Contexts   []ContextConfig  `yaml:"contexts,omitempty"   mapstructure:"contexts"`
}

type DatabaseType string

const (
	DatabaseSQLite   DatabaseType = "sqlite"
	DatabasePostgres DatabaseType = "postgres"
)

type AgentCapability string

const (
	CapabilityResumeCLI    AgentCapability = "resume.cli"
	CapabilityResumeStdin  AgentCapability = "resume.stdin"
	CapabilityResumeFile   AgentCapability = "resume.file"
	CapabilityReadsFiles   AgentCapability = "reads.files"
	CapabilityRunsCommands AgentCapability = "runs.commands"
)

type ContextType string

const (
	ContextFile      ContextType = "file"
	ContextDir       ContextType = "dir"
	ContextURL       ContextType = "url"
	ContextReference ContextType = "context"
)

// AgentConfig registers one coding agent Mnemo ingests from / can resume
// into. Kind selects the transcript parser for known agents; custom agents
// set Parser explicitly. Sources are path globs (supporting the {repo} token)
// that override the agent's built-in discovery location.
type AgentConfig struct {
	Name         string             `yaml:"name"                   mapstructure:"name"`
	Kind         domain.SessionKind `yaml:"kind"                   mapstructure:"kind"`
	Parser       domain.SessionKind `yaml:"parser,omitempty"       mapstructure:"parser"`
	Sources      []string           `yaml:"sources,omitempty"      mapstructure:"sources"`
	Capabilities []AgentCapability  `yaml:"capabilities,omitempty" mapstructure:"capabilities"`
}

// ContextConfig registers one non-session knowledge input. Type is
// file|dir|url|context; a context-type entry points at another context by
// Ref, forming a cycle-checked DAG resolved at compile time.
type ContextConfig struct {
	Name string      `yaml:"name"          mapstructure:"name"`
	Type ContextType `yaml:"type"          mapstructure:"type"`
	Path string      `yaml:"path,omitempty" mapstructure:"path"`
	Ref  string      `yaml:"ref,omitempty"  mapstructure:"ref"`
	URL  string      `yaml:"url,omitempty"  mapstructure:"url"`
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
	Type DatabaseType `yaml:"type" mapstructure:"type"`
	DSN  string       `yaml:"dsn"  mapstructure:"dsn"`
}

// EnrichmentConfig controls optional LLM-backed WorkingState refinement.
// It is disabled by default because enabling it may send task/session content
// to a configured local or cloud model endpoint.
type EnrichmentConfig struct {
	Enabled         bool    `yaml:"enabled,omitempty"          mapstructure:"enabled"`
	Provider        string  `yaml:"provider,omitempty"         mapstructure:"provider"`
	BaseURL         string  `yaml:"base_url,omitempty"         mapstructure:"base_url"`
	Model           string  `yaml:"model,omitempty"            mapstructure:"model"`
	APIKeyEnv       string  `yaml:"api_key_env,omitempty"      mapstructure:"api_key_env"`
	Timeout         string  `yaml:"timeout,omitempty"          mapstructure:"timeout"`
	MaxEvents       int     `yaml:"max_events,omitempty"       mapstructure:"max_events"`
	MaxEventChars   int     `yaml:"max_event_chars,omitempty"  mapstructure:"max_event_chars"`
	MaxInputChars   int     `yaml:"max_input_chars,omitempty"  mapstructure:"max_input_chars"`
	MaxOutputTokens int     `yaml:"max_output_tokens,omitempty" mapstructure:"max_output_tokens"`
	Temperature     float64 `yaml:"temperature,omitempty"      mapstructure:"temperature"`
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
	// AllowContextURLEgress permits url-type contexts to be fetched over the
	// network. Off by default: url contexts resolve to a placeholder until
	// the repo explicitly opts in.
	AllowContextURLEgress bool `yaml:"allow_context_url_egress,omitempty" mapstructure:"allow_context_url_egress"`
}

// Default returns the local-first default configuration: a single global
// SQLite database under the Mnemo home, shared across all projects and
// partitioned by repository identity.
func Default() Config {
	return Config{
		Database: DatabaseConfig{
			Type: "sqlite",
			DSN:  GlobalDBPath(),
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

// Save writes a full Mnemo config file without overwriting an existing file.
func Save(path string, cfg Config) error {
	return createFile(path, MarshalYAML(cfg))
}

// projectView is the per-project subset written to .mnemo/config.yaml.
// database is machine-level and lives only in the global config.
type projectView struct {
	Privacy    PrivacyConfig    `yaml:"privacy,omitempty"`
	Tasks      TasksConfig      `yaml:"tasks,omitempty"`
	Enrichment EnrichmentConfig `yaml:"enrichment,omitempty"`
	Agents     []AgentConfig    `yaml:"agents,omitempty"`
	Contexts   []ContextConfig  `yaml:"contexts,omitempty"`
}

// globalView is the machine-level subset written to ~/.mnemo/config.yaml.
type globalView struct {
	Database DatabaseConfig `yaml:"database"`
}

// SaveProject writes the per-project config, refusing to overwrite an
// existing file.
func SaveProject(path string, cfg Config) error {
	if err := validateProjectTypedFields(cfg); err != nil {
		return err
	}
	out, err := yaml.Marshal(projectView{
		Privacy:    cfg.Privacy,
		Tasks:      cfg.Tasks,
		Enrichment: cfg.Enrichment,
		Agents:     cfg.Agents,
		Contexts:   cfg.Contexts,
	})
	if err != nil {
		return err
	}
	return createFile(path, string(out))
}

// LoadProject reads only the per-project .mnemo/config.yaml (no global
// layering, strict parsing). A missing file yields an empty Config so
// `mnemo agents add` works on a not-yet-initialized project.
func LoadProject(repoRoot string) (Config, error) {
	path := DefaultPath(repoRoot)
	if !fileExists(path) {
		return Config{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()
	v := viper.New()
	v.SetConfigType(configType)
	if err := v.ReadConfig(file); err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := v.UnmarshalExact(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// RewriteProject overwrites the per-project config (used by `mnemo agents`
// and `mnemo context` to edit the agent/context lists in place).
func RewriteProject(path string, cfg Config) error {
	if err := validateProjectTypedFields(cfg); err != nil {
		return err
	}
	out, err := yaml.Marshal(projectView{
		Privacy:    cfg.Privacy,
		Tasks:      cfg.Tasks,
		Enrichment: cfg.Enrichment,
		Agents:     cfg.Agents,
		Contexts:   cfg.Contexts,
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// SaveGlobal writes the machine-level config if absent. An already-present
// global config is left untouched (returns nil) so `mnemo init` in a second
// project never clobbers machine settings.
func SaveGlobal(path string, cfg Config) error {
	if fileExists(path) {
		return nil
	}
	out, err := yaml.Marshal(globalView{
		Database: cfg.Database,
	})
	if err != nil {
		return err
	}
	return createFile(path, string(out))
}

func createFile(path string, content string) error {
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
	_, err = file.WriteString(content)
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
	v.SetDefault("database.type", string(def.Database.Type))
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
	if !validDatabaseType(cfg.Database.Type) {
		return Config{}, fmt.Errorf("unsupported database.type %q", cfg.Database.Type)
	}
	if cfg.Database.DSN == "" {
		return Config{}, fmt.Errorf("database.dsn is required")
	}
	if err := validateProjectTypedFields(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validDatabaseType(value DatabaseType) bool {
	switch value {
	case DatabaseSQLite, DatabasePostgres:
		return true
	default:
		return false
	}
}

func validCapability(value AgentCapability) bool {
	switch value {
	case CapabilityResumeCLI, CapabilityResumeStdin, CapabilityResumeFile, CapabilityReadsFiles, CapabilityRunsCommands:
		return true
	default:
		return false
	}
}

func validContextType(value ContextType) bool {
	switch value {
	case ContextFile, ContextDir, ContextURL, ContextReference:
		return true
	default:
		return false
	}
}

func validParser(value domain.SessionKind) bool {
	switch value {
	case "", "jsonl", "jsonl-openai", "jsonl-anthropic":
		return true
	default:
		return false
	}
}

func validateProjectTypedFields(cfg Config) error {
	for _, agent := range cfg.Agents {
		for _, capability := range agent.Capabilities {
			if !validCapability(capability) {
				return fmt.Errorf("agent %q has unsupported capability %q", agent.Name, capability)
			}
		}
		if !validParser(agent.Parser) {
			return fmt.Errorf("agent %q has unsupported parser %q", agent.Name, agent.Parser)
		}
	}
	for _, ctx := range cfg.Contexts {
		if !validContextType(ctx.Type) {
			return fmt.Errorf("context %q has unsupported type %q", ctx.Name, ctx.Type)
		}
	}
	return nil
}
