package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var pol = Policy{Feature: "feature/MID-1", Dev: "feature/MID-1-dev", Base: "release", PRBase: "dev"}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "config", "user.name", "t")
	run(t, dir, "config", "user.email", "t@example.com")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func withOrigin(t *testing.T, dir string) string {
	t.Helper()
	bare, _ := filepath.EvalSymlinks(t.TempDir())
	run(t, bare, "init", "-q", "--bare", "-b", "main")
	run(t, dir, "remote", "add", "origin", bare)
	return bare
}

func split(s string) []string { return strings.Fields(s) }

func TestValidateGitAllowsTheFlow(t *testing.T) {
	for _, c := range []string{
		"switch -c feature/MID-1 release", "switch -c feature/MID-1 origin/release", "switch -c feature/MID-1-dev origin/dev", "switch -c feature/MID-1-dev dev",
		"switch feature/MID-1", "merge --no-edit feature/MID-1",
		"push -u origin feature/MID-1-dev", "push origin feature/MID-1",
		"status --porcelain", "status -s", "status", "status -b",
		"log --oneline -n 5", "log --oneline main..feature/MID-1", "diff --stat main..feature/MID-1",
		"fetch --prune origin", "fetch", "fetch origin",
		"branch --show-current", "branch --list feature/*", "branch -r",
		"rev-parse --abbrev-ref HEAD", "rev-parse --verify main",
	} {
		if err := ValidateGit(split(c), pol); err != nil {
			t.Errorf("%q rejected: %v", c, err)
		}
	}
}

func TestValidateGitRejects(t *testing.T) {
	for _, c := range []string{
		"push --force origin feature/MID-1", "push -f origin feature/MID-1", "push --force-with-lease origin feature/MID-1",
		"push origin dev", "push origin main", "push origin master", "push origin feature/other",
		"push origin :feature/MID-1", "push origin feature/MID-1 feature/MID-1-dev", "push --delete origin feature/MID-1",
		"push -u origin dev", "push upstream feature/MID-1",
		"branch -D x", "branch -d x", "branch -m x", "reset --hard", "clean -fd", "checkout .", "restore .", "rebase main",
		"switch --detach", "switch -c other main", "switch -c feature/MID-1 main", "switch -c feature/MID-1-dev release", "switch -c feature/MID-1 --force", "switch -c feature/MID-1 -x",
		"merge feature/other", "merge --no-edit dev", "merge --no-edit", "merge --squash feature/MID-1",
		"fetch --upload-pack=x", "fetch origin main", "log --all", "log -p", "log --oneline -n x", "diff main..feature/MID-1",
		"diff --stat --output=x main..feature/MID-1", "--version", "-c core.pager=x status", "status ; rm -rf /",
		"rev-parse --git-dir", "rev-parse --verify -x", "branch --list -x", "stash", "config user.name x",
		"switch ../x", "switch -", "switch --orphan x",
	} {
		if err := ValidateGit(split(c), pol); err == nil {
			t.Errorf("%q should be rejected", c)
		}
	}
	if err := ValidateGit(nil, pol); err == nil {
		t.Error("empty args must be rejected")
	}
}

func TestGitRuntimeGuards(t *testing.T) {
	repo := newRepo(t)
	run(t, repo, "branch", "release")
	run(t, repo, "switch", "-q", "-c", "elsewhere")
	run(t, repo, "commit", "-q", "--allow-empty", "-m", "elsewhere")
	os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644)
	if _, err := Git(repo, split("switch -c feature/MID-1 release"), pol); err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("dirty switch: %v", err)
	}
	os.Remove(filepath.Join(repo, "dirty.txt"))
	run(t, repo, "switch", "-q", "main")
	run(t, repo, "branch", "-q", "-D", "elsewhere")
	if out, err := Git(repo, split("switch -c feature/MID-1 release"), pol); err != nil {
		t.Fatalf("switch -c: %v\n%s", err, out)
	}
	if got := run(t, repo, "branch", "--show-current"); got != "feature/MID-1" {
		t.Fatalf("current = %q", got)
	}
	if _, err := Git(repo, split("switch -c feature/MID-1 release"), pol); err == nil {
		t.Error("existing branch must refuse switch -c")
	}
	if _, err := Git(repo, split("merge --no-edit feature/MID-1"), pol); err == nil || !strings.Contains(err.Error(), "base branch") {
		t.Errorf("merge off the dev branch: %v", err)
	}
}

func TestGitMergeAndPush(t *testing.T) {
	repo := newRepo(t)
	run(t, repo, "branch", "dev")
	bare := withOrigin(t, repo)
	run(t, repo, "switch", "-q", "-c", pol.Feature)
	os.WriteFile(filepath.Join(repo, "f.txt"), []byte("f"), 0o644)
	run(t, repo, "add", "-A")
	run(t, repo, "commit", "-q", "-m", "work")
	run(t, repo, "switch", "-q", "main")
	if out, err := Git(repo, split("switch -c "+pol.Dev+" dev"), pol); err != nil {
		t.Fatalf("make dev: %v\n%s", err, out)
	}
	if out, err := Git(repo, split("merge --no-edit "+pol.Feature), pol); err != nil {
		t.Fatalf("merge: %v\n%s", err, out)
	}
	if _, err := Git(repo, split("push -u origin "+pol.Dev), pol); err == nil {
		t.Fatal("push of -dev must wait for PushReady")
	}
	ready := pol
	ready.PushReady = true
	if out, err := Git(repo, split("push -u origin "+pol.Dev), ready); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	if got := run(t, bare, "branch", "--list", pol.Dev); !strings.Contains(got, pol.Dev) {
		t.Fatalf("origin lacks the branch: %q", got)
	}
}

func TestGHRules(t *testing.T) {
	good := []string{"pr", "create", "--base", "dev", "--head", pol.Dev, "--title", "t", "--body", "b"}
	if err := ValidateGH(good, pol); err != nil {
		t.Errorf("good create rejected: %v", err)
	}
	if err := ValidateGH([]string{"pr", "view", pol.Dev}, pol); err != nil {
		t.Errorf("view rejected: %v", err)
	}
	for _, bad := range [][]string{
		{"pr", "merge", pol.Dev},
		{"pr", "create", "--base", "main", "--head", pol.Dev, "--title", "t", "--body", "b"},
		{"pr", "create", "--base", "dev", "--head", "other", "--title", "t", "--body", "b"},
		{"pr", "create", "--base", "dev", "--head", pol.Dev, "--title", strings.Repeat("x", 201), "--body", "b"},
		{"pr", "create", "--base", "dev", "--head", pol.Dev, "--title", "t", "--body", strings.Repeat("x", 10001)},
		{"pr", "create", "--base", "dev", "--head", pol.Dev, "--title", "t", "--body", "b", "--web"},
		{"pr", "view", "other"}, {"repo", "delete"}, {"api", "x"}, {},
	} {
		if err := ValidateGH(bad, pol); err == nil {
			t.Errorf("%v should be rejected", bad)
		}
	}
	oldKind := RemoteKind
	defer func() { RemoteKind = oldKind }()
	RemoteKind = func(string) string { return "other" }
	if _, err := GH(t.TempDir(), good, pol); err == nil || !strings.Contains(err.Error(), "not on GitHub") {
		t.Errorf("non-GitHub remote: %v", err)
	}
	RemoteKind = func(string) string { return "github" }
	if _, err := GH(t.TempDir(), good, pol); err == nil || !strings.Contains(err.Error(), "integrated") {
		t.Errorf("not integrated: %v", err)
	}
	pol := pol
	pol.Integrated = true
	t.Setenv("PATH", t.TempDir())
	if _, err := GH(t.TempDir(), good, pol); err == nil || !strings.Contains(err.Error(), "gh is not installed") {
		t.Errorf("without gh: %v", err)
	}
	if _, err := GH(t.TempDir(), []string{"pr", "merge", "x"}, pol); err == nil || strings.Contains(err.Error(), "not installed") {
		t.Errorf("rule check must come first: %v", err)
	}
}

func TestValidateGHDirectRepoPrsFeatureIntoBase(t *testing.T) {
	p := Policy{Feature: "feature/X-1", Dev: "feature/X-1-dev", Base: "release/1.2", PRBase: "dev", Direct: true}
	ok := []string{"pr", "create", "--base", "release/1.2", "--head", "feature/X-1", "--title", "X-1: t", "--body", "b"}
	if err := ValidateGH(ok, p); err != nil {
		t.Fatalf("direct PR refused: %v", err)
	}
	bad := []string{"pr", "create", "--base", "dev", "--head", "feature/X-1-dev", "--title", "X-1: t", "--body", "b"}
	if err := ValidateGH(bad, p); err == nil {
		t.Fatal("a direct repo must not open the -dev PR")
	}
	if err := ValidateGH([]string{"pr", "view", "feature/X-1"}, p); err != nil {
		t.Fatalf("view of the feature branch refused: %v", err)
	}
	via := Policy{Feature: "feature/X-1", Dev: "feature/X-1-dev", Base: "release/1.2", PRBase: "dev"}
	if err := ValidateGH(ok, via); err == nil {
		t.Fatal("a via-dev repo must not open the direct PR")
	}
}
