// Package gitx holds the guarded git helpers: branch-name checks, repo lookup and (later) the
// allow-list that `claude -p` goes through when it runs git for the owner.
package gitx

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var branchRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// ValidBranch reports whether name is a safe branch name (never an option, never a ref trick).
func ValidBranch(name string) bool {
	return branchRe.MatchString(name) && !strings.Contains(name, "..") && !strings.Contains(name, "//") &&
		!strings.HasSuffix(name, "/") && !strings.HasSuffix(name, ".lock") && !strings.Contains(name, "/.")
}

// IsRepo reports whether path is a directory holding .git (a directory, or a file for worktrees).
func IsRepo(path string) bool {
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		return false
	}
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// RepoRootWithin returns the nearest ancestor of path, up to root inclusive, that is a git repo.
// Symlinks are resolved first, so a link that leaves root is rejected.
func RepoRootWithin(root, path string) (string, error) {
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(r, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("that folder is outside the board folder")
	}
	for d := p; ; d = filepath.Dir(d) {
		if IsRepo(d) {
			return d, nil
		}
		if d == r || d == filepath.Dir(d) {
			return "", errors.New("no git repository found there")
		}
	}
}
