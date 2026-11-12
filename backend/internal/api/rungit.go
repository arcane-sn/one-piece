package api

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// ticketRepos returns the ticket's Branching rows in status "branched": the repos an Agent Task
// works in and commits to. Empty means Run behaves as it did before branching existed.
func (s *server) ticketRepos(cardID int64) ([]store.Branch, error) {
	all, err := s.st.ListBranches(cardID)
	if err != nil {
		return nil, err
	}
	var out []store.Branch
	for _, r := range all {
		if r.Status == "branched" {
			out = append(out, r)
		}
	}
	return out, nil
}

// repoGit runs a plain git command in repo (read-only calls and the per-task commit).
func repoGit(repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return s, fmt.Errorf("%v: %s", err, shorten(s, 300))
	}
	return s, nil
}

func onFeature(r store.Branch) error {
	cur, err := repoGit(r.Repo, "branch", "--show-current")
	if err != nil {
		return store.ConflictError{Msg: fmt.Sprintf("Cannot read the branch of %s: %v", filepath.Base(r.Repo), err)}
	}
	if cur == "" {
		cur = "no branch (detached HEAD)"
	}
	if cur != r.FeatureBranch {
		return store.ConflictError{Msg: fmt.Sprintf("%s is on %s, expected %s. Switch it (Discuss or git) and run again.", filepath.Base(r.Repo), cur, r.FeatureBranch)}
	}
	return nil
}

// checkOnFeature requires every repo to be on its feature branch.
func checkOnFeature(repos []store.Branch) error {
	for _, r := range repos {
		if err := onFeature(r); err != nil {
			return err
		}
	}
	return nil
}

// commitTask commits what an Agent Task changed, one commit per repo with changes:
// "<task ident>: <title>". It refuses when a repo has left its feature branch meanwhile.
func commitTask(repos []store.Branch, it store.Item) (changed []string, err error) {
	msg := it.Ident + ": " + shorten(it.Title, 120)
	for _, r := range repos {
		if err := onFeature(r); err != nil {
			return changed, fmt.Errorf("%s: %v", filepath.Base(r.Repo), err)
		}
		st, err := repoGit(r.Repo, "status", "--porcelain")
		if err != nil {
			return changed, fmt.Errorf("%s: %v", filepath.Base(r.Repo), err)
		}
		if st == "" {
			continue
		}
		if _, err := repoGit(r.Repo, "add", "-A"); err != nil {
			return changed, fmt.Errorf("%s: %v", filepath.Base(r.Repo), err)
		}
		if _, err := repoGit(r.Repo, "commit", "-q", "-m", msg); err != nil {
			return changed, fmt.Errorf("%s: %v", filepath.Base(r.Repo), err)
		}
		changed = append(changed, filepath.Base(r.Repo))
	}
	return changed, nil
}
