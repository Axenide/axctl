package config

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

const maxImportDepth = 10

// DefaultConfigPath returns the resolved path to the axctl config file.
// Checks $XDG_CONFIG_HOME/axctl/config.toml first, falls back to ~/.config/axctl/config.toml.
func DefaultConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "axctl", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".config", "axctl", "config.toml")
	}
	return filepath.Join(home, ".config", "axctl", "config.toml")
}

// LoadConfig loads and merges a TOML configuration file, resolving imports.
func LoadConfig(path string) (*TOMLConfig, error) {
	resolved, err := resolvePath(path)
	if err != nil {
		return nil, fmt.Errorf("resolving config path: %w", err)
	}

	visited := make(map[string]bool)
	return loadRecursive(resolved, visited, 0)
}

// ResolveIncludePaths returns all file paths referenced by the config (main + includes).
// Used by the watcher to know which files to monitor.
func ResolveIncludePaths(path string) []string {
	resolved, err := resolvePath(path)
	if err != nil {
		return []string{path}
	}

	paths := []string{resolved}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return paths
	}

	var cfg TOMLConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return paths
	}

	baseDir := filepath.Dir(resolved)
	visited := map[string]bool{resolved: true}
	collectIncludePaths(baseDir, cfg.Include, visited, 0, &paths)
	return paths
}

func collectIncludePaths(baseDir string, includes []string, visited map[string]bool, depth int, out *[]string) {
	if depth >= maxImportDepth {
		return
	}
	for _, pattern := range includes {
		resolvedPattern := filepath.Join(baseDir, pattern)
		matches, err := filepath.Glob(resolvedPattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			abs, err := filepath.Abs(match)
			if err != nil {
				continue
			}
			if visited[abs] {
				continue
			}
			visited[abs] = true
			*out = append(*out, abs)

			// Recurse into this file's includes
			data, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			var sub TOMLConfig
			if err := toml.Unmarshal(data, &sub); err != nil {
				continue
			}
			collectIncludePaths(filepath.Dir(abs), sub.Include, visited, depth+1, out)
		}
	}
}

func loadRecursive(absPath string, visited map[string]bool, depth int) (*TOMLConfig, error) {
	if depth > maxImportDepth {
		return nil, fmt.Errorf("max import depth (%d) exceeded at %s", maxImportDepth, absPath)
	}
	if visited[absPath] {
		return nil, fmt.Errorf("circular import detected: %s", absPath)
	}
	visited[absPath] = true

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", absPath, err)
	}

	var cfg TOMLConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", absPath, err)
	}

	if len(cfg.Include) == 0 {
		return &cfg, nil
	}

	// Resolve includes relative to this file's directory
	baseDir := filepath.Dir(absPath)
	var merged TOMLConfig

	for _, pattern := range cfg.Include {
		resolvedPattern := filepath.Join(baseDir, pattern)
		matches, err := filepath.Glob(resolvedPattern)
		if err != nil {
			fmt.Printf("[axctl-config] Warning: invalid glob pattern %q: %v\n", pattern, err)
			continue
		}
		if len(matches) == 0 {
			fmt.Printf("[axctl-config] Warning: include %q matched no files\n", pattern)
			continue
		}
		for _, match := range matches {
			matchAbs, err := filepath.Abs(match)
			if err != nil {
				continue
			}
			sub, err := loadRecursive(matchAbs, visited, depth+1)
			if err != nil {
				fmt.Printf("[axctl-config] Warning: skipping include %s: %v\n", match, err)
				continue
			}
			mergeConfig(&merged, sub)
		}
	}

	// Main file values override all includes
	cfg.Include = nil
	mergeConfig(&merged, &cfg)

	return &merged, nil
}

func resolvePath(path string) (string, error) {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	return filepath.Abs(path)
}

// mergeConfig overlays src onto dst. Non-nil values in src override dst.
// Scalar fields are overwritten; array fields (keybinds, window rules) are appended.
func mergeConfig(dst, src *TOMLConfig) {
	if src.Appearance != nil {
		if dst.Appearance == nil {
			dst.Appearance = &AppearanceConfig{}
		}
		mergeAppearance(dst.Appearance, src.Appearance)
	}

	if src.Input != nil {
		if dst.Input == nil {
			dst.Input = &InputConfig{}
		}
		if src.Input.Keyboard != nil {
			dst.Input.Keyboard = src.Input.Keyboard
		}
	}

	if len(src.Keybinds) > 0 {
		dst.Keybinds = append(dst.Keybinds, src.Keybinds...)
	}

	if len(src.WindowRules) > 0 {
		dst.WindowRules = append(dst.WindowRules, src.WindowRules...)
	}
}

func mergeAppearance(dst, src *AppearanceConfig) {
	if src.Gaps != nil {
		if dst.Gaps == nil {
			dst.Gaps = &GapsConfig{}
		}
		if src.Gaps.Inner != nil {
			dst.Gaps.Inner = src.Gaps.Inner
		}
		if src.Gaps.Outer != nil {
			dst.Gaps.Outer = src.Gaps.Outer
		}
	}
	if src.Border != nil {
		if dst.Border == nil {
			dst.Border = &BorderConfig{}
		}
		if src.Border.Width != nil {
			dst.Border.Width = src.Border.Width
		}
		if src.Border.ActiveColor != nil {
			dst.Border.ActiveColor = src.Border.ActiveColor
		}
		if src.Border.InactiveColor != nil {
			dst.Border.InactiveColor = src.Border.InactiveColor
		}
		if src.Border.Rounding != nil {
			dst.Border.Rounding = src.Border.Rounding
		}
	}
	if src.Opacity != nil {
		if dst.Opacity == nil {
			dst.Opacity = &OpacityConfig{}
		}
		if src.Opacity.Active != nil {
			dst.Opacity.Active = src.Opacity.Active
		}
		if src.Opacity.Inactive != nil {
			dst.Opacity.Inactive = src.Opacity.Inactive
		}
	}
	if src.Blur != nil {
		if dst.Blur == nil {
			dst.Blur = &BlurConfig{}
		}
		if src.Blur.Enabled != nil {
			dst.Blur.Enabled = src.Blur.Enabled
		}
		if src.Blur.Size != nil {
			dst.Blur.Size = src.Blur.Size
		}
		if src.Blur.Passes != nil {
			dst.Blur.Passes = src.Blur.Passes
		}
	}
	if src.Shadow != nil {
		if dst.Shadow == nil {
			dst.Shadow = &ShadowConfig{}
		}
		if src.Shadow.Enabled != nil {
			dst.Shadow.Enabled = src.Shadow.Enabled
		}
		if src.Shadow.Size != nil {
			dst.Shadow.Size = src.Shadow.Size
		}
		if src.Shadow.Color != nil {
			dst.Shadow.Color = src.Shadow.Color
		}
	}
	if src.Animations != nil {
		if dst.Animations == nil {
			dst.Animations = &AnimationsConfig{}
		}
		if src.Animations.Enabled != nil {
			dst.Animations.Enabled = src.Animations.Enabled
		}
	}
}
