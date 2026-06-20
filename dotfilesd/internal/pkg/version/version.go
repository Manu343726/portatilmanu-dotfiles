package version

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Info struct {
	Hash     string
	Title    string
	Branch   string
	TagName  string
	TagTitle string
}

// Get runs git commands in repoDir (~/dotfilesd) to collect version info.
func Get(repoDir string) Info {
	info := Info{}

	if out, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output(); err == nil {
		info.Hash = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "log", "-1", "--format=%s").Output(); err == nil {
		info.Title = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		info.Branch = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "describe", "--tags", "--exact-match").Output(); err == nil {
		info.TagName = strings.TrimSpace(string(out))
		if info.TagName != "" {
			if out, err := exec.Command("git", "-C", repoDir, "tag", "-l", "--format=%(contents:subject)", info.TagName).Output(); err == nil {
				info.TagTitle = strings.TrimSpace(string(out))
			}
		}
	}

	return info
}

func (v Info) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("commit:  %s", v.Hash))
	if v.Title != "" {
		b.WriteString(fmt.Sprintf("\ntitle:  %s", v.Title))
	}
	if v.Branch != "" {
		b.WriteString(fmt.Sprintf("\nbranch: %s", v.Branch))
	}
	if v.TagName != "" {
		b.WriteString(fmt.Sprintf("\ntag:    %s", v.TagName))
		if v.TagTitle != "" {
			b.WriteString(fmt.Sprintf("\n  title: %s", v.TagTitle))
		}
	}
	return b.String()
}

func repoDir() string {
	if d := os.Getenv("DOTFILESD_DIR"); d != "" {
		return d
	}
	return os.Getenv("HOME") + "/dotfilesd"
}

func Print(name string) {
	info := Get(repoDir())
	if info.Hash == "" {
		fmt.Printf("%s: unknown (not a git repo or git unavailable)\n", name)
		return
	}
	fmt.Printf("%s\n%s\n", name, info.String())
}
