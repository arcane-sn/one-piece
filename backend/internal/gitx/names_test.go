package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestValidBranch(t *testing.T) {
	for _, ok := range []string{"feature/MID-1", "dev", "release/1.2", "feature/MID-1-dev"} {
		if !ValidBranch(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "-x", "--upload-pack=x", "a b", "a..b", "a//b", "a/", "x.lock", "~x", "a:b", "feature/x.lock"} {
		if ValidBranch(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
}

func TestRepoRootWithin(t *testing.T) {
	master, _ := filepath.EvalSymlinks(t.TempDir())
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	repoA := filepath.Join(master, "repoA")
	gitInit(t, repoA)
	os.MkdirAll(filepath.Join(repoA, "src"), 0o755)
	os.MkdirAll(filepath.Join(master, "plain"), 0o755)
	gitInit(t, filepath.Join(outside, "evil"))
	os.Symlink(filepath.Join(outside, "evil"), filepath.Join(master, "link"))

	if got, err := RepoRootWithin(master, filepath.Join(repoA, "src")); err != nil || got != repoA {
		t.Errorf("src → %q, %v", got, err)
	}
	if _, err := RepoRootWithin(master, filepath.Join(master, "plain")); err == nil {
		t.Error("plain dir must error")
	}
	if _, err := RepoRootWithin(master, filepath.Join(master, "..", filepath.Base(outside), "evil")); err == nil {
		t.Error("outside path must error")
	}
	if _, err := RepoRootWithin(master, filepath.Join(master, "link")); err == nil {
		t.Error("symlink escaping the master folder must error")
	}
	gitInit(t, filepath.Join(master, "x")) // keep master itself a non-repo for the cases above
	m2, _ := filepath.EvalSymlinks(t.TempDir())
	gitInit(t, m2)
	if got, err := RepoRootWithin(m2, m2); err != nil || got != m2 {
		t.Errorf("master itself → %q, %v", got, err)
	}
	if IsRepo(filepath.Join(master, "plain")) || !IsRepo(repoA) {
		t.Error("IsRepo wrong")
	}
}
