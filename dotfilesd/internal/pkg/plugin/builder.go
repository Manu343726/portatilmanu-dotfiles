package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Builder compiles Go plugin sources and manages a binary cache.
//
// Workflow:
//  1. Hash all source files (go.mod, go.sum, *.go) in the plugin directory
//  2. Compare hash against the cached value
//  3. If unchanged, return the cached binary path
//  4. If changed or missing, compile and store the new binary + hash
type Builder struct {
	CacheDir string
}

// BuildResult describes the outcome of a plugin build.
type BuildResult struct {
	BinaryPath string // absolute path to the compiled binary
	FromCache  bool   // true if binary was served from cache
}

// Build compiles a plugin from sourceDir and caches the binary in the
// builder's CacheDir/<name>/ directory.
func (b *Builder) Build(pluginName, sourceDir string) (*BuildResult, error) {
	cacheDir := filepath.Join(b.CacheDir, pluginName)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	binaryPath := filepath.Join(cacheDir, pluginName)
	hashPath := filepath.Join(cacheDir, ".hash")

	// Compute current source hash.
	currentHash, err := hashSource(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("hash source: %w", err)
	}

	// Check cache.
	if cachedHash, err := os.ReadFile(hashPath); err == nil {
		if strings.TrimSpace(string(cachedHash)) == currentHash {
			if _, err := os.Stat(binaryPath); err == nil {
				return &BuildResult{BinaryPath: binaryPath, FromCache: true}, nil
			}
		}
	}

	// Build.
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = sourceDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// Try go mod tidy first, then rebuild.
		tidy := exec.Command("go", "mod", "tidy")
		tidy.Dir = sourceDir
		tidy.Stdout = os.Stdout
		tidy.Stderr = os.Stderr
		_ = tidy.Run()

		cmd2 := exec.Command("go", "build", "-o", binaryPath, ".")
		cmd2.Dir = sourceDir
		cmd2.Stdout = os.Stdout
		cmd2.Stderr = os.Stderr
		if err2 := cmd2.Run(); err2 != nil {
			return nil, fmt.Errorf("build plugin %q: %w (after tidy: %v)", pluginName, err, err2)
		}
	}

	// Store hash.
	if err := os.WriteFile(hashPath, []byte(currentHash+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write hash: %w", err)
	}

	return &BuildResult{BinaryPath: binaryPath, FromCache: false}, nil
}

// hashSource computes a SHA-256 hash of all Go source files, go.mod, and
// go.sum in the given directory.
func hashSource(dir string) (string, error) {
	h := sha256.New()

	// Collect files to hash.
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip hidden dirs (like .git, .cache).
			if strings.HasPrefix(d.Name(), ".") && path != dir {
				return fs.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(d.Name())
		if ext == ".go" || d.Name() == "go.mod" || d.Name() == "go.sum" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk source dir: %w", err)
	}
	sort.Strings(files)

	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		h.Write([]byte(rel + "\x00"))

		fh, err := os.Open(f)
		if err != nil {
			return "", fmt.Errorf("open %s: %w", rel, err)
		}
		if _, err := io.Copy(h, fh); err != nil {
			fh.Close()
			return "", fmt.Errorf("hash %s: %w", rel, err)
		}
		fh.Close()
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
