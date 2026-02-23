package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const FileName = "ws.toml"
const LocalFileName = "ws.local.toml"

type Config struct {
	Workspace WorkspaceConfig       `toml:"workspace"`
	Repos     map[string]RepoConfig `toml:"repos"`
	Boarded   map[string][]string   `toml:"-"` // from ws.local.toml [boarded] section
	Git       string                `toml:"-"` // from ws.local.toml only
}

type LocalConfig struct {
	Git     string                `toml:"git"`
	Repos   map[string]RepoConfig `toml:"repos"`
	Boarded map[string][]string   `toml:"boarded"`
}

type WorkspaceConfig struct {
	Org           string `toml:"org"`
	DefaultBranch string `toml:"default_branch"`
	DisplayName   string `toml:"display_name"`
}

type RepoConfig struct {
	DisplayName string   `toml:"display_name"`
	Aliases     []string `toml:"aliases"`
	Color       string   `toml:"color"`
	PostCreate  string   `toml:"post_create"`
}

func Parse(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
}

func Discover(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, FileName)
		if _, err := os.Stat(candidate); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%s not found in any parent directory", FileName)
		}
		dir = parent
	}
}

// Merge returns a new Config with local overrides applied on top of base.
// Only repos that exist in base are considered; unknown repos in local are skipped.
// Aliases are appended; PostCreate, Color, and DisplayName replace if non-empty.
func Merge(base, local *Config) *Config {
	merged := &Config{
		Workspace: base.Workspace,
		Repos:     make(map[string]RepoConfig, len(base.Repos)),
	}
	maps.Copy(merged.Repos, base.Repos)
	for name, localRepo := range local.Repos {
		baseRepo, ok := merged.Repos[name]
		if !ok {
			continue
		}
		if localRepo.PostCreate != "" {
			baseRepo.PostCreate = localRepo.PostCreate
		}
		if localRepo.Color != "" {
			baseRepo.Color = localRepo.Color
		}
		if localRepo.DisplayName != "" {
			baseRepo.DisplayName = localRepo.DisplayName
		}
		if len(localRepo.Aliases) > 0 {
			baseRepo.Aliases = append(baseRepo.Aliases, localRepo.Aliases...)
		}
		merged.Repos[name] = baseRepo
	}
	return merged
}

func Load(startDir string) (*Config, string, error) {
	root, err := Discover(startDir)
	if err != nil {
		return nil, "", err
	}
	cfg, err := Parse(filepath.Join(root, FileName))
	if err != nil {
		return nil, "", err
	}

	cfg.Boarded = make(map[string][]string)

	localPath := filepath.Join(root, LocalFileName)
	if _, err := os.Stat(localPath); err == nil {
		var local LocalConfig
		if _, err := toml.DecodeFile(localPath, &local); err != nil {
			return nil, "", fmt.Errorf("parsing %s: %w", LocalFileName, err)
		}

		// Merge repo overrides using existing Merge function
		localCfg := &Config{Repos: local.Repos}
		cfg = Merge(cfg, localCfg)

		// Preserve Boarded from local config
		if local.Boarded != nil {
			cfg.Boarded = local.Boarded
		}

		if local.Git != "" {
			cfg.Git = local.Git
		}
	}

	if cfg.Git != "" && cfg.Git != "ssh" && cfg.Git != "https" {
		return nil, "", fmt.Errorf("invalid git protocol %q in %s: must be \"ssh\" or \"https\"", cfg.Git, LocalFileName)
	}

	return cfg, root, nil
}

// SaveBoarded writes the boarded state to ws.local.toml, preserving existing repo overrides.
func SaveBoarded(root string, boarded map[string][]string) error {
	path := filepath.Join(root, LocalFileName)

	// Read existing local config (if any)
	var local LocalConfig
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &local); err != nil {
			return fmt.Errorf("parsing %s: %w", LocalFileName, err)
		}
	}

	local.Boarded = boarded

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(local)
}
