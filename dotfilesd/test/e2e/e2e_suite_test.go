package e2e

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)

	initTestLogging()

	RunSpecs(t, "E2E Suite")
}

func initTestLogging() {
	logPath := resolveTestLogPath()
	os.MkdirAll(filepath.Dir(logPath), 0755)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		panic("failed to open test log: " + err.Error())
	}
	handler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))
}

// resolveTestLogPath walks up from the test package directory to find the
// module root (where go.mod lives), then returns logs/unittests.log.
func resolveTestLogPath() string {
	dir, err := os.Getwd()
	if err != nil {
		return "logs/unittests.log"
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "logs", "unittests.log")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "logs/unittests.log"
}
