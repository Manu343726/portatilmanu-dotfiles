package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Front matter structures
// ---------------------------------------------------------------------------

// ScriptFrontMatter is YAML front matter embedded at the top of a .dsh file.
type ScriptFrontMatter struct {
	Description string              `yaml:"description"`
	Params      []ScriptParamYAML   `yaml:"params"`
}

// ScriptParamYAML describes one parameter in front matter.
type ScriptParamYAML struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Default     string `yaml:"default"`
}

// DirFrontMatter is YAML front matter from a README.md in a script directory.
type DirFrontMatter struct {
	Description string   `yaml:"description"`
	Enabled     bool     `yaml:"enabled"`
	Exclude     []string `yaml:"exclude"`
}

// ---------------------------------------------------------------------------
// Script registry
// ---------------------------------------------------------------------------

// ScriptRegistry holds the scanned tree of registered scripts.
type ScriptRegistry struct {
	rootDir string
}

// NewScriptRegistry creates a registry rooted at dir.
func NewScriptRegistry(dir string) *ScriptRegistry {
	return &ScriptRegistry{rootDir: dir}
}

// RootDir returns the configured scripts directory path.
func (r *ScriptRegistry) RootDir() string {
	return r.rootDir
}

// ListScripts returns the full tree as protobuf messages.
func (r *ScriptRegistry) ListScripts() ([]*dotfilesdv1.ScriptEntry, error) {
	return r.scanDir(r.rootDir, "", nil)
}

// ResolveScriptPath converts a registered script path (e.g. "git/commit")
// to an absolute file path. Returns empty string if not found.
func (r *ScriptRegistry) ResolveScriptPath(relPath string) string {
	// Sanitize: no leading/trailing slashes, no .dsh extension in input.
	relPath = strings.Trim(relPath, "/")
	relPath = strings.TrimSuffix(relPath, ".dsh")

	abs := filepath.Join(r.rootDir, relPath+".dsh")
	if _, err := os.Stat(abs); err == nil {
		return abs
	}

	// Try without .dsh for directories (shouldn't happen but be safe).
	abs = filepath.Join(r.rootDir, relPath)
	if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
		return abs
	}

	return ""
}

// ReadScriptContent reads and returns the script content (without front matter)
// for a registered script path.
func (r *ScriptRegistry) ReadScriptContent(relPath string) (string, string, error) {
	abs := r.ResolveScriptPath(relPath)
	if abs == "" {
		return "", "", fmt.Errorf("registered script not found: %s", relPath)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", "", fmt.Errorf("read script %s: %w", relPath, err)
	}
	content, fm := splitFrontMatter(string(data))
	return content, fm, nil
}

// scanDir recursively scans a directory and returns ScriptEntry items.
// readmeConfig carries the parent README's exclude/enabled settings.
func (r *ScriptRegistry) scanDir(dir, prefix string, readmeConfig *DirFrontMatter) ([]*dotfilesdv1.ScriptEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read scripts dir %s: %w", dir, err)
	}

	// Check for README.md in this directory.
	dirFM := readmeConfig
	if rm := findReadme(entries); rm != nil {
		dfm, err := parseDirFrontMatter(filepath.Join(dir, rm.Name()))
		if err != nil {
			slog.Warn("parse README front matter", "path", rm.Name(), "error", err)
		} else {
			dirFM = dfm
		}
	}

	// Collect entries that are not excluded.
	var result []*dotfilesdv1.ScriptEntry

	// Directories first (sorted).
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)

	for _, d := range dirs {
		childPrefix := d
		if prefix != "" {
			childPrefix = prefix + "/" + d
		}

		children, err := r.scanDir(filepath.Join(dir, d), childPrefix, dirFM)
		if err != nil {
			slog.Warn("scan scripts subdir", "dir", d, "error", err)
			continue
		}
		if children == nil {
			children = []*dotfilesdv1.ScriptEntry{}
		}

		// Check if this group is excluded.
		if dirFM != nil && stringInSlice(d, dirFM.Exclude) {
			continue
		}

		desc := ""
		if dirFM != nil {
			desc = dirFM.Description
		}

		enabled := true
		if dirFM != nil {
			enabled = dirFM.Enabled
		}

		result = append(result, &dotfilesdv1.ScriptEntry{
			Path:        childPrefix,
			Name:        d,
			IsDirectory: true,
			Description: desc,
			Enabled:     enabled,
			Children:    children,
		})
	}

	// Script files (sorted).
	var files []string
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".dsh") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		name := strings.TrimSuffix(f, ".dsh")
		childPath := name
		if prefix != "" {
			childPath = prefix + "/" + name
		}

		// Check if this script is excluded.
		if dirFM != nil && stringInSlice(name, dirFM.Exclude) {
			continue
		}

		enabled := true
		if dirFM != nil {
			enabled = dirFM.Enabled
		}

		// Parse front matter for description and params.
		fm, params := parseScriptFrontMatter(filepath.Join(dir, f))
		desc := fm.Description
		if desc == "" {
			// Use name as fallback description.
			desc = name
		}

		var protoParams []*dotfilesdv1.ScriptParam
		for _, p := range params {
			protoParams = append(protoParams, &dotfilesdv1.ScriptParam{
				Name:         p.Name,
				Description:  p.Description,
				Required:     p.Required,
				DefaultValue: p.Default,
			})
		}

		result = append(result, &dotfilesdv1.ScriptEntry{
			Path:        childPath,
			Name:        name,
			IsDirectory: false,
			Description: desc,
			Enabled:     enabled,
			Params:      protoParams,
		})
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Front matter parsing helpers
// ---------------------------------------------------------------------------

// splitFrontMatter splits raw text into (bodyWithoutFrontMatter, yamlString).
// Returns ("", "") if no front matter is present.
func splitFrontMatter(text string) (body string, frontMatter string) {
	text = strings.TrimLeft(text, "\n\r\t ")
	if !strings.HasPrefix(text, "---") {
		return text, ""
	}

	rest := text[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return text, ""
	}

	frontMatter = strings.TrimSpace(rest[:idx])
	body = strings.TrimLeft(rest[idx+4:], "\n\r")
	return body, frontMatter
}

// parseScriptFrontMatter reads a .dsh file, extracts its YAML front matter,
// and returns the parsed metadata.
func parseScriptFrontMatter(path string) (ScriptFrontMatter, []ScriptParamYAML) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ScriptFrontMatter{}, nil
	}

	_, yamlStr := splitFrontMatter(string(data))
	if yamlStr == "" {
		return ScriptFrontMatter{}, nil
	}

	var fm ScriptFrontMatter
	if err := yaml.Unmarshal([]byte(yamlStr), &fm); err != nil {
		slog.Debug("parse script front matter", "path", path, "error", err)
		return ScriptFrontMatter{}, nil
	}

	return fm, fm.Params
}

// parseDirFrontMatter reads a README.md and extracts its YAML front matter.
func parseDirFrontMatter(path string) (*DirFrontMatter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	_, yamlStr := splitFrontMatter(string(data))
	if yamlStr == "" {
		// README exists but no front matter — enabled by default.
		return &DirFrontMatter{Enabled: true, Exclude: nil}, nil
	}

	var fm DirFrontMatter
	if err := yaml.Unmarshal([]byte(yamlStr), &fm); err != nil {
		return nil, err
	}

	return &fm, nil
}

// findReadme looks for a README.md entry in a directory listing.
func findReadme(entries []os.DirEntry) os.DirEntry {
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), "README.md") {
			return e
		}
	}
	return nil
}

// stringInSlice checks if a string is in a slice.
func stringInSlice(s string, slice []string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
