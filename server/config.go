package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModelSetting stores per-model context/thinking choices made in the dashboard.
type ModelSetting struct {
	Context  string `json:"context,omitempty"`  // "" | 200K | 400K | 1M
	Thinking string `json:"thinking,omitempty"` // "" | off | on | <effort: low/medium/high/max/xhigh>
}

// Config persists dashboard-managed runtime settings next to the binary.
type Config struct {
	Pats          []string                `json:"pats"`
	ActivePAT     string                  `json:"active_pat"`
	DefaultModel  string                  `json:"default_model"`
	ModelSettings map[string]ModelSetting `json:"model_settings,omitempty"`

	path string
}

// configPath prefers the working directory (survives `go run` temp binaries),
// then the executable directory.
func configPath() string {
	if _, err := os.Stat("config.json"); err == nil {
		return "config.json"
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "config.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "config.json"
}

// LoadConfig reads config.json; missing file is not an error.
func LoadConfig() *Config {
	cfg := &Config{path: configPath()}
	raw, err := os.ReadFile(cfg.path)
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		fmt.Printf("[config] parse %s failed: %v\n", cfg.path, err)
	}
	cfg.path = configPath()
	return cfg
}

func (c *Config) Save() error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.path, raw, 0o600); err != nil {
		return err
	}
	return nil
}

// PatsFromEnv parses QODER_PAT_LIST (comma/newline separated) with QODER_PAT fallback.
func PatsFromEnv() []string {
	if list := os.Getenv("QODER_PAT_LIST"); strings.TrimSpace(list) != "" {
		parts := strings.FieldsFunc(list, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == ';'
		})
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if pat := strings.TrimSpace(os.Getenv("QODER_PAT")); pat != "" {
		return []string{pat}
	}
	return nil
}
