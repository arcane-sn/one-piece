package store

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func repoBoard(t *testing.T) (*Store, Board, Card, string) {
	t.Helper()
	s := openTemp(t)
	master, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(master, "svc")
	os.MkdirAll(repo, 0o755)
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	b, err := s.CreateBoard("Midas", "", []string{master})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.CreateCard(b.ID, "Login", "", "")
	return s, b, c, repo
}

func TestPlanBranchNamesAndValidation(t *testing.T) {
	s, b, c, repo := repoBoard(t)
	if b.BranchPrefix != "feature/" || b.DevBranch != "dev" {
		t.Fatalf("defaults = %q %q", b.BranchPrefix, b.DevBranch)
	}
	br, err := s.PlanBranch(c.ID, repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	if br.FeatureBranch != "feature/MID-1" || br.DevBranch != "feature/MID-1-dev" || br.BaseBranch != "main" || br.Status != "planned" || br.Repo != repo {
		t.Fatalf("branch = %+v", br)
	}
	// re-planning a planned row updates the base
	if br, err = s.PlanBranch(c.ID, repo, "develop"); err != nil || br.BaseBranch != "develop" {
		t.Fatalf("replan = %+v, %v", br, err)
	}
	st := "branched"
	s.UpdateBranch(c.ID, repo, BranchPatch{Status: &st})
	if _, err := s.PlanBranch(c.ID, repo, "main"); !errors.As(err, &ConflictError{}) {
		t.Fatalf("replan after start: %v", err)
	}

	outside := t.TempDir()
	for name, args := range map[string][2]string{
		"outside": {outside, "main"},
		"nonrepo": {filepath.Dir(repo), "main"},
		"badbase": {repo, "--upload-pack=x"},
	} {
		if _, err := s.PlanBranch(c.ID, args[0], args[1]); !errors.As(err, &ValidationError{}) {
			t.Errorf("%s: err = %v", name, err)
		}
	}

	nb, err := s.SetBoardBranchDefaults(b.ID, "work/", "integration")
	if err != nil || nb.BranchPrefix != "work/" || nb.DevBranch != "integration" {
		t.Fatalf("defaults = %+v, %v", nb, err)
	}
	for _, bad := range [][2]string{{"Work/", "dev"}, {"work", "dev"}, {"work/", "a b"}, {"", "dev"}} {
		if _, err := s.SetBoardBranchDefaults(b.ID, bad[0], bad[1]); !errors.As(err, &ValidationError{}) {
			t.Errorf("defaults %v: err = %v", bad, err)
		}
	}
	c2, _ := s.CreateCard(b.ID, "Other", "", "")
	br2, _ := s.PlanBranch(c2.ID, repo, "main")
	if br2.FeatureBranch != "work/MID-2" || br2.DevBranch != "work/MID-2-dev" {
		t.Fatalf("custom prefix = %+v", br2)
	}

	if err := s.DeleteCard(c.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.ListBranches(c.ID); len(l) != 0 {
		t.Fatalf("cascade left %+v", l)
	}
	if l, _ := s.ListBranches(c2.ID); len(l) != 1 {
		t.Fatalf("other card lost its row: %+v", l)
	}
}

func TestUpdateBranchStatus(t *testing.T) {
	s, _, c, repo := repoBoard(t)
	s.PlanBranch(c.ID, repo, "main")
	for _, st := range []string{"planned", "branched", "merged_dev", "pr_open", "blocked"} {
		st := st
		if br, err := s.UpdateBranch(c.ID, repo, BranchPatch{Status: &st}); err != nil || br.Status != st {
			t.Errorf("status %q: %+v, %v", st, br, err)
		}
	}
	weird := "weird"
	if _, err := s.UpdateBranch(c.ID, repo, BranchPatch{Status: &weird}); !errors.As(err, &ValidationError{}) {
		t.Errorf("weird status: %v", err)
	}
	url, note := "https://example.com/pr/1", "waiting"
	br, err := s.UpdateBranch(c.ID, repo, BranchPatch{PRURL: &url, Note: &note})
	if err != nil || br.PRURL != url || br.Note != note {
		t.Fatalf("patch = %+v, %v", br, err)
	}
	bad := "a b"
	if _, err := s.UpdateBranch(c.ID, repo, BranchPatch{FeatureBranch: &bad}); !errors.As(err, &ValidationError{}) {
		t.Errorf("bad feature name: %v", err)
	}
	if _, err := s.UpdateBranch(c.ID, "/nope", BranchPatch{Note: &note}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown repo: %v", err)
	}
	if err := s.DeleteBranch(c.ID, repo); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBranch(c.ID, repo); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}
