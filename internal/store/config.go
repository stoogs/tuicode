// Package store manages tuicode's own configuration: the app config and
// per-model config files under ~/.config/tuicode (overridable for testing).
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ResidencyMode is how a model's keep_alive is managed.
type ResidencyMode string

const (
	AutoUnload ResidencyMode = "auto_unload"
	KeepWarm   ResidencyMode = "keep_warm"
	Manual     ResidencyMode = "manual"
)

// Residency is a model's keep-alive policy.
type Residency struct {
	Mode        ResidencyMode `json:"mode"`
	IdleMinutes int           `json:"idle_minutes,omitempty"`
}

// KeepAlive renders the residency policy as an Ollama keep_alive value string.
//
//	auto_unload → "<n>m"   keep_warm → "-1"   manual → "24h" (long; user unloads)
func (r Residency) KeepAlive() string {
	switch r.Mode {
	case KeepWarm:
		return "-1"
	case Manual:
		return "24h"
	default: // AutoUnload
		m := r.IdleMinutes
		if m <= 0 {
			m = 20
		}
		return itoa(m) + "m"
	}
}

// DefaultResidency is the global default: auto-unload after 20 min idle.
func DefaultResidency() Residency {
	return Residency{Mode: AutoUnload, IdleMinutes: 20}
}

// Parameters are sampler settings.
type Parameters struct {
	Temperature float64 `json:"temperature"`
	TopP        float64 `json:"top_p"`
	TopK        int     `json:"top_k"`
}

// AppConfig is tuicode's global configuration.
type AppConfig struct {
	DeviceMode       string    `json:"device_mode"` // auto | cpu-only | gpu-only
	DefaultResidency Residency `json:"default_residency"`
	OpencodeJSON     string    `json:"opencode_json,omitempty"`    // chosen target path
	WorkingDir       string    `json:"working_dir,omitempty"`      // dir to launch OpenCode in
	Favourite        string    `json:"favourite,omitempty"`        // model tag selected by default on startup
	TrendingFetched  int64     `json:"trending_fetched,omitempty"` // unix secs of last trending refresh
	// DefaultContext is the global default context (num_ctx) a model uses when
	// its own context is 0 ("follow the global default"). 0 = auto (model
	// default). Set/changed in 4k increments in Settings.
	DefaultContext int `json:"default_context,omitempty"`
	// Compaction controls OpenCode's conversation-compaction behavior.
	Compaction Compaction `json:"compaction"`
}

// Compaction mirrors OpenCode's top-level "compaction" config, written into
// opencode.json on launch. It keeps a long session usable within the context
// window: OpenCode summarizes (and optionally prunes) older messages as the
// window fills, leaving `reserved` tokens of headroom.
type Compaction struct {
	Auto       bool `json:"auto"`        // auto-summarize when the window fills
	Prune      bool `json:"prune"`       // drop old tool outputs to save tokens
	ReservePct int  `json:"reserve_pct"` // headroom reserved as a % of the context window (→ compaction triggers earlier)
}

// DefaultAppConfig returns sane defaults.
func DefaultAppConfig() AppConfig {
	return AppConfig{
		DeviceMode:       "auto",
		DefaultResidency: DefaultResidency(),
		Compaction:       Compaction{Auto: true, Prune: true, ReservePct: 25},
	}
}

// Store is rooted at a config directory (default ~/.config/tuicode).
type Store struct {
	Dir string
}

// New returns a Store rooted at dir. If dir is empty, it uses
// $XDG_CONFIG_HOME/tuicode or ~/.config/tuicode.
func New(dir string) (*Store, error) {
	if dir == "" {
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			base = filepath.Join(home, ".config")
		}
		dir = filepath.Join(base, "tuicode")
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) configPath() string { return filepath.Join(s.Dir, "config.json") }
func (s *Store) modelsDir() string  { return filepath.Join(s.Dir, "models") }
func (s *Store) BackupsDir() string { return filepath.Join(s.Dir, "backups") }

// LoadAppConfig reads config.json, returning defaults if it doesn't exist.
func (s *Store) LoadAppConfig() (AppConfig, error) {
	data, err := os.ReadFile(s.configPath())
	if os.IsNotExist(err) {
		return DefaultAppConfig(), nil
	}
	if err != nil {
		return DefaultAppConfig(), err
	}
	cfg := DefaultAppConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultAppConfig(), err
	}
	return cfg, nil
}

// SaveAppConfig writes config.json (creating the dir).
func (s *Store) SaveAppConfig(cfg AppConfig) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.configPath(), append(data, '\n'), 0o644)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// writeFileAtomic writes via a temp file + rename in the same dir.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
