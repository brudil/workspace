package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const FileName = "ws.toml"
const LocalFileName = "ws.local.toml"

type Config struct {
	Workspace WorkspaceConfig        `toml:"workspace"`
	Repos     map[string]RepoConfig  `toml:"repos"`
	Groups    map[string]GroupConfig `toml:"groups"`
	Boarded   map[string][]string    `toml:"-"` // from ws.local.toml [boarded] section
	Git       string                 `toml:"-"` // from ws.local.toml only
	Silo      map[string]string      `toml:"-"` // from ws.local.toml [silo] section
}

type LocalConfig struct {
	Git     string                 `toml:"git"`
	Repos   map[string]RepoConfig  `toml:"repos"`
	Groups  map[string]GroupConfig `toml:"groups,omitempty"`
	Boarded map[string][]string    `toml:"boarded"`
	Silo    map[string]string      `toml:"silo"`
}

// GroupConfig controls a repo group. Hidden groups are left out of commands
// that cover every repo unless asked for with --group. Hidden is a pointer so
// ws.local.toml can tell "not set" apart from an explicit false.
type GroupConfig struct {
	Hidden *bool `toml:"hidden,omitempty"`
}

const RepoFileName = "ws.repo.toml"

type SiloRepoConfig struct {
	AfterSwitch string `toml:"after_switch"`
	AfterChange string `toml:"after_change"`
}

type RepoFileConfig struct {
	Capsule CapsuleConfig  `toml:"capsule"`
	Silo    SiloRepoConfig `toml:"silo"`
}

type CapsuleConfig struct {
	CopyFromGround []string `toml:"copy_from_ground"`
	AfterCreate    string   `toml:"after_create"`
}

// ParseRepoConfig parses a ws.repo.toml file at the given path.
// Returns nil, nil if the file does not exist.
func ParseRepoConfig(path string) (*RepoFileConfig, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	var cfg RepoFileConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
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
	AfterCreate string   `toml:"after_create"`
	Group       string   `toml:"group,omitempty"`
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
// Aliases are appended; AfterCreate, Color, DisplayName, and Group replace if non-empty.
// Group visibility set locally replaces the base setting; local groups may be
// new, so a repo can be moved into a group the team config never declared.
func Merge(base, local *Config) *Config {
	merged := &Config{
		Workspace: base.Workspace,
		Repos:     make(map[string]RepoConfig, len(base.Repos)),
		Groups:    make(map[string]GroupConfig, len(base.Groups)),
	}
	maps.Copy(merged.Repos, base.Repos)
	maps.Copy(merged.Groups, base.Groups)
	for name, localGroup := range local.Groups {
		if localGroup.Hidden != nil {
			merged.Groups[name] = localGroup
		}
	}
	for name, localRepo := range local.Repos {
		baseRepo, ok := merged.Repos[name]
		if !ok {
			continue
		}
		if localRepo.AfterCreate != "" {
			baseRepo.AfterCreate = localRepo.AfterCreate
		}
		if localRepo.Color != "" {
			baseRepo.Color = localRepo.Color
		}
		if localRepo.DisplayName != "" {
			baseRepo.DisplayName = localRepo.DisplayName
		}
		if localRepo.Group != "" {
			baseRepo.Group = localRepo.Group
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
	cfg.Silo = make(map[string]string)

	localPath := filepath.Join(root, LocalFileName)
	if _, err := os.Stat(localPath); err == nil {
		var local LocalConfig
		if _, err := toml.DecodeFile(localPath, &local); err != nil {
			return nil, "", fmt.Errorf("parsing %s: %w", LocalFileName, err)
		}

		// Merge repo overrides using existing Merge function
		localCfg := &Config{Repos: local.Repos, Groups: local.Groups}
		cfg = Merge(cfg, localCfg)

		// Preserve Boarded from local config
		if local.Boarded != nil {
			cfg.Boarded = local.Boarded
		}

		if local.Silo != nil {
			cfg.Silo = local.Silo
		}

		if local.Git != "" {
			cfg.Git = local.Git
		}
	}

	if cfg.Git != "" && cfg.Git != "ssh" && cfg.Git != "https" {
		return nil, "", fmt.Errorf("invalid git protocol %q in %s: must be \"ssh\" or \"https\"", cfg.Git, LocalFileName)
	}

	for name := range cfg.Groups {
		if err := validateGroupName(name); err != nil {
			return nil, "", err
		}
	}
	for repo, rc := range cfg.Repos {
		if rc.Group == "" {
			continue
		}
		if err := validateGroupName(rc.Group); err != nil {
			return nil, "", fmt.Errorf("repo %q: %w", repo, err)
		}
	}

	return cfg, root, nil
}

// validateGroupName rejects names that can't be passed to --group, which
// takes a comma-separated list.
func validateGroupName(name string) error {
	if name == "" || strings.ContainsAny(name, ", \t") {
		return fmt.Errorf("invalid group name %q: must be non-empty with no commas or spaces", name)
	}
	return nil
}

// IsHidden reports whether the named group is hidden. Groups with no
// setting are visible.
func (c *Config) IsHidden(group string) bool {
	g, ok := c.Groups[group]
	return ok && g.Hidden != nil && *g.Hidden
}

func UpdateLocal(root string, fn func(*LocalConfig)) error {
	path := filepath.Join(root, LocalFileName)

	var local LocalConfig
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &local); err != nil {
			return fmt.Errorf("parsing %s: %w", LocalFileName, err)
		}
	}

	fn(&local)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(local)
}

func SaveBoarded(root string, boarded map[string][]string) error {
	return UpdateLocal(root, func(local *LocalConfig) {
		local.Boarded = boarded
	})
}

func SaveSilo(root string, silo map[string]string) error {
	return UpdateLocal(root, func(local *LocalConfig) {
		local.Silo = silo
	})
}

// SaveGroupVisibility records a personal visibility choice for a group in
// ws.local.toml. A nil hidden removes the choice, so the team default applies.
func SaveGroupVisibility(root, group string, hidden *bool) error {
	return UpdateLocal(root, func(local *LocalConfig) {
		if hidden == nil {
			delete(local.Groups, group)
			return
		}
		if local.Groups == nil {
			local.Groups = make(map[string]GroupConfig)
		}
		local.Groups[group] = GroupConfig{Hidden: hidden}
	})
}
