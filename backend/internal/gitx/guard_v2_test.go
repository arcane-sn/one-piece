package gitx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func v2pol() Policy {
	p := pol
	p.Ident = "MID-1"
	p.Mode = "merge"
	return p
}

func TestValidateGitV2Allows(t *testing.T) {
	p := v2pol()
	for _, c := range [][]string{
		{"checkout", "feature/MID-1"}, {"checkout", "release"},
		{"checkout", "-b", "feature/MID-1", "origin/release"}, {"checkout", "-b", "feature/MID-1-dev", "dev"},
		{"pull", "--ff-only"},
		{"stash", "push", "-u", "-m", "MID-1 wip before closeout"}, {"stash", "list"}, {"stash", "pop"},
		{"merge", "--abort"}, {"merge", "--no-edit", "release"}, {"merge", "--no-edit", "origin/release"},
		{"merge", "--no-edit", "origin/dev"}, {"merge", "--no-edit", "feature/MID-1"},
		{"cherry-pick", "-x", "abc1234"}, {"cherry-pick", "-x", "abc1234", strings.Repeat("a", 40)}, {"cherry-pick", "--abort"},
	} {
		if err := ValidateGit(c, p); err != nil {
			t.Errorf("%v rejected: %v", c, err)
		}
	}
}

func TestValidateGitV2Rejects(t *testing.T) {
	p := v2pol()
	for _, c := range [][]string{
		{"pull"}, {"pull", "origin", "dev"}, {"pull", "--rebase"}, {"pull", "--ff-only", "origin"},
		{"stash"}, {"stash", "drop"}, {"stash", "clear"}, {"stash", "apply"}, {"stash", "pop", "stash@{1}"},
		{"stash", "push", "-u", "-m", "no ticket here"}, {"stash", "push", "-u", "-m", "MID-12 other ticket"},
		{"stash", "push", "-m", "MID-1 x"}, {"stash", "push", "-u", "-m", "MID-1; rm -rf"}, {"stash", "push", "-a", "-m", "MID-1 x"},
		{"checkout", "--", "file"}, {"checkout", "-f", "x"}, {"checkout", "-B", "feature/MID-1", "release"}, {"checkout", "."},
		{"checkout", "-b", "other", "release"}, {"checkout", "-b", "feature/MID-1", "dev"}, {"checkout", "-b", "feature/MID-1-dev", "release"},
		{"merge", "dev"}, {"merge", "--no-edit", "dev"}, {"merge", "--no-edit", "feature/MID-1-dev"}, {"merge", "--no-edit", "main"},
		{"merge", "--squash", "feature/MID-1"}, {"merge", "--abort", "x"},
		{"cherry-pick", "abc1234"}, {"cherry-pick", "-x"}, {"cherry-pick", "-m", "1", "abc1234"}, {"cherry-pick", "--continue"},
		{"cherry-pick", "-x", "HEAD"}, {"cherry-pick", "-x", "abc"}, {"cherry-pick", "-x", "abc1234..def5678"},
		{"reset", "--hard"}, {"rebase", "main"}, {"clean", "-fd"}, {"branch", "-D", "x"}, {"clone", "x"}, {"worktree", "add", "x"},
		{"commit", "-m", "x"}, {"push", "--force", "origin", "feature/MID-1"}, {"push", "origin", "dev"},
	} {
		if err := ValidateGit(c, p); err == nil {
			t.Errorf("%v should be rejected", c)
		}
	}
	bad := p
	bad.Base = "dev"
	bad.PRBase = "dev"
	if err := ValidateGit([]string{"merge", "--no-edit", "dev"}, bad); err == nil {
		t.Error("base == dev must never be mergeable into the feature")
	}
}

func commit(t *testing.T, repo, file, msg string) string {
	t.Helper()
	os.WriteFile(filepath.Join(repo, file), []byte(msg), 0o644)
	run(t, repo, "add", "-A")
	run(t, repo, "commit", "-q", "-m", msg)
	return run(t, repo, "rev-parse", "HEAD")
}

// repoFlow: release and dev branches, a feature branch with two own commits, now on a -dev branch cut from dev.
func repoFlow(t *testing.T) (string, Policy, []string) {
	repo := newRepo(t)
	p := v2pol()
	run(t, repo, "branch", "release")
	run(t, repo, "branch", "dev")
	run(t, repo, "switch", "-q", "-c", p.Feature, "release")
	c1 := commit(t, repo, "f1.txt", "one")
	c2 := commit(t, repo, "f2.txt", "two")
	run(t, repo, "switch", "-q", "dev")
	commit(t, repo, "d.txt", "dev moved")
	run(t, repo, "switch", "-q", "-c", p.Dev)
	return repo, p, []string{c1, c2}
}

func TestCherryPickRules(t *testing.T) {
	repo, p, shas := repoFlow(t)
	if _, err := Git(repo, []string{"cherry-pick", "-x", shas[0]}, p); err == nil || !strings.Contains(err.Error(), "set the mode") {
		t.Fatalf("merge mode must refuse cherry-pick: %v", err)
	}
	p.Mode = "cherry-pick"
	if _, err := Git(repo, split("merge --no-edit "+p.Feature), p); err == nil || !strings.Contains(err.Error(), "cherry-pick") {
		t.Fatalf("cherry-pick mode must refuse merge: %v", err)
	}
	pend, err := PendingPicks(repo, p)
	if err != nil || len(pend) != 2 || pend[0].SHA != shas[0] || pend[0].Subject != "one" {
		t.Fatalf("pending = %v, %v", pend, err)
	}
	if _, err := Git(repo, []string{"cherry-pick", "-x", shas[1], shas[0]}, p); err == nil || !strings.Contains(err.Error(), "oldest first") {
		t.Errorf("order: %v", err)
	}
	devSHA := run(t, repo, "rev-parse", "dev")
	if _, err := Git(repo, []string{"cherry-pick", "-x", devSHA[:10]}, p); err == nil || !strings.Contains(err.Error(), "own commits") {
		t.Errorf("outside the range: %v", err)
	}
	r, err := GitX(repo, []string{"cherry-pick", "-x", shas[0][:10]}, p)
	if err != nil || len(r.Picked) != 1 || r.Picked[0] != shas[0] {
		t.Fatalf("pick: %v %+v", err, r)
	}
	p.Picked = r.Picked
	if _, err := Git(repo, []string{"cherry-pick", "-x", shas[0]}, p); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("already picked: %v", err)
	}
	if pend, _ := PendingPicks(repo, p); len(pend) != 1 || pend[0].SHA != shas[1] {
		t.Errorf("pending after pick = %v", pend)
	}
	// a merge commit on the feature branch is never a candidate
	run(t, repo, "switch", "-q", p.Feature)
	run(t, repo, "merge", "-q", "--no-edit", "-s", "ours", "dev")
	merge := run(t, repo, "rev-parse", "HEAD")
	run(t, repo, "switch", "-q", p.Dev)
	if _, err := Git(repo, []string{"cherry-pick", "-x", merge}, p); err == nil {
		t.Error("merge commit must be refused")
	}
}

func TestCherryPickConflictAbort(t *testing.T) {
	repo, p, shas := repoFlow(t)
	p.Mode = "cherry-pick"
	commit(t, repo, "f1.txt", "different") // the -dev branch now conflicts with the first feature commit
	if _, err := Git(repo, []string{"cherry-pick", "-x", shas[0]}, p); err == nil {
		t.Fatal("expected a conflict")
	}
	if _, err := Git(repo, split("cherry-pick --abort"), p); err != nil { // a dirty tree is fine for abort
		t.Fatalf("abort: %v", err)
	}
	if run(t, repo, "status", "--porcelain") != "" {
		t.Error("abort must leave a clean tree")
	}
}

func TestMergeRules(t *testing.T) {
	repo, p, _ := repoFlow(t)
	if _, err := Git(repo, split("merge --no-edit "+p.Feature), p); err != nil {
		t.Fatalf("merge feature: %v", err)
	}
	run(t, repo, "switch", "-q", p.Feature)
	if _, err := Git(repo, split("merge --no-edit feature/MID-1-dev"), p); err == nil {
		t.Error("-dev into feature must be refused")
	}
	if _, err := Git(repo, split("merge --no-edit release"), p); err != nil {
		t.Errorf("base into feature: %v", err)
	}
	run(t, repo, "switch", "-q", "release")
	if _, err := Git(repo, split("merge --no-edit "+p.Feature), p); err == nil {
		t.Error("merge off the ticket branches must be refused")
	}
}

func TestMergeConflictAbortAndDirty(t *testing.T) {
	repo, p, _ := repoFlow(t)
	run(t, repo, "switch", "-q", p.Feature)
	commit(t, repo, "d.txt", "feature version")
	run(t, repo, "switch", "-q", p.Dev)
	commit(t, repo, "d.txt", "dev version")
	if out, err := Git(repo, split("merge --no-edit "+p.Feature), p); err == nil || !strings.Contains(out, "CONFLICT") {
		t.Fatalf("expected conflict: %v %s", err, out)
	}
	if _, err := Git(repo, split("merge --no-edit "+p.Feature), p); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("dirty tree must refuse a new merge: %v", err)
	}
	if _, err := Git(repo, split("merge --abort"), p); err != nil {
		t.Fatalf("abort: %v", err)
	}
}

func TestCheckoutAndPull(t *testing.T) {
	repo := newRepo(t)
	p := v2pol()
	run(t, repo, "branch", "release")
	withOrigin(t, repo)
	run(t, repo, "push", "-q", "origin", "release")
	run(t, repo, "fetch", "-q", "origin")
	if _, err := Git(repo, split("checkout -b feature/MID-1 origin/release"), p); err != nil {
		t.Fatalf("checkout -b: %v", err)
	}
	if up, err := gitRun(repo, 5e9, "rev-parse", "--abbrev-ref", "@{upstream}"); err == nil {
		t.Errorf("--no-track expected, upstream = %q", up)
	}
	if _, err := Git(repo, split("pull --ff-only"), p); err == nil || !strings.Contains(err.Error(), "push -u") {
		t.Errorf("pull without own upstream: %v", err)
	}
	run(t, repo, "push", "-q", "-u", "origin", p.Feature)
	if out, err := Git(repo, split("pull --ff-only"), p); err != nil {
		t.Errorf("pull: %v %s", err, out)
	}
	if _, err := Git(repo, split("checkout release"), p); err != nil {
		t.Errorf("checkout release: %v", err)
	}
	if _, err := Git(repo, split("pull --ff-only"), p); err == nil {
		t.Error("pull off the ticket branches must be refused")
	}
	if _, err := Git(repo, split("checkout a.txt"), p); err == nil {
		t.Error("a path is not a branch")
	}
	if _, err := Git(repo, split("checkout "+run(t, repo, "rev-parse", "HEAD")), p); err == nil {
		t.Error("a sha is not a branch")
	}
	os.WriteFile(filepath.Join(repo, "x"), []byte("x"), 0o644)
	if _, err := Git(repo, split("checkout "+p.Feature), p); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("dirty checkout: %v", err)
	}
}

func TestStash(t *testing.T) {
	repo := newRepo(t)
	p := v2pol()
	run(t, repo, "branch", "release")
	run(t, repo, "switch", "-q", "-c", p.Feature, "release")
	os.WriteFile(filepath.Join(repo, "w.txt"), []byte("w"), 0o644)
	r, err := GitX(repo, []string{"stash", "push", "-u", "-m", "MID-1 wip"}, p) // a dirty tree is allowed here
	if err != nil || r.Stash == "" || r.StashBranch != p.Feature {
		t.Fatalf("stash: %v %+v", err, r)
	}
	if r2, err := GitX(repo, []string{"stash", "push", "-u", "-m", "MID-1 again"}, p); err != nil || r2.Stash != "" {
		t.Errorf("nothing to stash must record nothing: %v %+v", err, r2)
	}
	if out, _ := Git(repo, split("stash list"), p); !strings.Contains(out, "MID-1 wip") {
		t.Errorf("list = %q", out)
	}
	other := p
	other.Ident = "MID-12"
	if _, err := Git(repo, split("stash pop"), other); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Errorf("another ticket's stash: %v", err)
	}
	run(t, repo, "switch", "-q", "release")
	if _, err := Git(repo, split("stash pop"), p); err == nil || !strings.Contains(err.Error(), "another branch") {
		t.Errorf("other branch: %v", err)
	}
	run(t, repo, "switch", "-q", p.Feature)
	r, err = GitX(repo, split("stash pop"), p)
	if err != nil || r.Popped == "" {
		t.Fatalf("pop: %v %+v", err, r)
	}
	if _, err := Git(repo, split("stash pop"), p); err == nil {
		t.Error("nothing left to pop")
	}
}

func TestPushGate(t *testing.T) {
	repo := newRepo(t)
	p := v2pol()
	withOrigin(t, repo)
	run(t, repo, "branch", "release")
	run(t, repo, "switch", "-q", "-c", p.Feature, "release")
	if _, err := Git(repo, split("push -u origin "+p.Feature), p); err != nil {
		t.Fatalf("backup push before integration: %v", err)
	}
	p.Integrated = true
	if _, err := Git(repo, split("push origin "+p.Feature), p); err == nil {
		t.Error("an integrated repo may not push while another repo is not ready")
	}
	p.PushReady = true
	if _, err := Git(repo, split("push origin "+p.Feature), p); err != nil {
		t.Errorf("ready push: %v", err)
	}
}

func TestRemoteKindOf(t *testing.T) {
	for u, want := range map[string]string{
		"https://github.com/o/r.git": "github", "git@github.com:o/r.git": "github", "ssh://git@github.com/o/r": "github",
		"https://user:tok@github.com/o/r": "github", "https://gitlab.example.test/o/r.git": "other", "git@bitbucket.example.test:o/r.git": "other",
		"https://notgithub.com/o/r": "other", "/srv/git/r.git": "other",
	} {
		if got := remoteKindOf(u); got != want {
			t.Errorf("%s = %s, want %s", u, got, want)
		}
	}
}
