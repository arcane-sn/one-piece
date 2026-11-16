package qa

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// RepoState is what a run records about each repo under test.
type RepoState struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
	Dirty  bool   `json:"dirty"`
	Status string `json:"-"` // git status --porcelain, for the before/after backstop
}

func git(repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// Snapshot records branch, HEAD and the working-tree status of one repo.
func Snapshot(repo string) (RepoState, error) {
	s := RepoState{Repo: repo}
	var err error
	if s.SHA, err = git(repo, "rev-parse", "HEAD"); err != nil {
		return s, err
	}
	s.Branch, _ = git(repo, "branch", "--show-current")
	if s.Status, err = git(repo, "status", "--porcelain"); err != nil {
		return s, err
	}
	s.Dirty = s.Status != ""
	return s, nil
}

// Changed lists the repos whose HEAD or working-tree status differs between two snapshots.
func Changed(before, after []RepoState) []string {
	was := map[string]RepoState{}
	for _, b := range before {
		was[b.Repo] = b
	}
	var out []string
	for _, a := range after {
		if b, ok := was[a.Repo]; ok && (b.SHA != a.SHA || b.Status != a.Status) {
			out = append(out, a.Repo)
		}
	}
	return out
}
