package config

import (
	"os"
	"path/filepath"
)

// The single global Mnemo home holds the default solo database and the
// machine-level config. Per-project .mnemo/config.yaml then overlays this
// with agents, contexts, and privacy.
const (
	globalDirName    = ".mnemo"
	globalDBFileName = "mnemo.db"
	// HomeEnv lets tests and power users relocate the entire global home.
	HomeEnv = "MNEMO_HOME"
)

// GlobalDir resolves the machine-level Mnemo home:
//
//	$MNEMO_HOME                     (explicit override; used by tests)
//	$XDG_DATA_HOME/mnemo            (XDG, when set)
//	~/.mnemo                        (default)
//
// It never returns an error: an undiscoverable home falls back to a relative
// .mnemo so the CLI still functions in degraded environments.
func GlobalDir() string {
	if v := os.Getenv(HomeEnv); v != "" {
		return v
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "mnemo")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return globalDirName
	}
	return filepath.Join(home, globalDirName)
}

// GlobalConfigPath is the machine-level config file path.
func GlobalConfigPath() string {
	return filepath.Join(GlobalDir(), DefaultFileName)
}

// GlobalDBPath is the default solo SQLite database path.
func GlobalDBPath() string {
	return filepath.Join(GlobalDir(), globalDBFileName)
}

// LoadLayered builds the effective config: defaults, then the global config
// file (if present), then the project .mnemo/config.yaml (if present), with
// later layers overriding earlier ones. Strict parsing (unknown-key
// rejection) is preserved on every layer.
func LoadLayered(repoRoot string) (Config, error) {
	v := newViper()

	read := false
	if gp := GlobalConfigPath(); fileExists(gp) {
		v.SetConfigFile(gp)
		if err := v.ReadInConfig(); err != nil {
			return Config{}, err
		}
		read = true
	}

	if pp := DefaultPath(repoRoot); fileExists(pp) {
		file, err := os.Open(pp)
		if err != nil {
			return Config{}, err
		}
		defer file.Close()
		if read {
			if err := v.MergeConfig(file); err != nil {
				return Config{}, err
			}
		} else if err := v.ReadConfig(file); err != nil {
			return Config{}, err
		}
	}

	return unmarshal(v)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
