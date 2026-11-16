package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func g(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@x"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestCherryPickFlowRecordsAndModeRule(t *testing.T) {
	st, env, repo, card := branchFixture(t)
	f, d := "feature/"+card.Ident, "feature/"+card.Ident+"-dev"
	g(t, repo, "branch", "release")
	g(t, repo, "branch", "dev")
	runCLI(env, "", "branch-plan", repo, "release")
	if _, err := runCLI(env, "", "git", repo, "checkout", "-b", f, "release"); err != nil {
		t.Fatal(err)
	}
	runCLI(env, "", "branch-status", repo, "branched")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0o644)
	g(t, repo, "add", "-A")
	g(t, repo, "commit", "-q", "-m", "first")
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b"), 0o644)
	g(t, repo, "add", "-A")
	g(t, repo, "commit", "-q", "-m", "second")
	if _, err := runCLI(env, "", "git", repo, "checkout", "-b", d, "origin/dev"); err == nil {
		t.Fatal("no origin here: start point must fail")
	}
	if _, err := runCLI(env, "", "git", repo, "checkout", "-b", d, "dev"); err != nil {
		t.Fatal(err)
	}
	// Claude cannot jump to cherry-pick without a blocked repo
	if _, err := runCLI(env, "", "branch-mode", repo, "cherry-pick"); err == nil || !strings.Contains(err.Error(), "only accepted after") {
		t.Fatalf("mode switch without conflict: %v", err)
	}
	if _, err := runCLI(env, "", "git", repo, "cherry-pick", "-x", "abc1234"); err == nil {
		t.Fatal("cherry-pick in merge mode")
	}
	runCLI(env, "", "branch-status", repo, "blocked", "merge", "conflict")
	if out, err := runCLI(env, "", "branch-mode", repo, "cherry-pick"); err != nil || !strings.Contains(out, "cherry-pick") {
		t.Fatalf("mode: %q %v", out, err)
	}
	pend, err := runCLI(env, "", "pending-picks", repo)
	lines := strings.Split(strings.TrimSpace(pend), "\n")
	if err != nil || len(lines) != 2 || !strings.HasSuffix(lines[0], " first") {
		t.Fatalf("pending = %q %v", pend, err)
	}
	sha1, sha2 := strings.Fields(lines[0])[0], strings.Fields(lines[1])[0]
	if out, err := runCLI(env, "", "git", repo, "cherry-pick", "-x", sha1); err != nil {
		t.Fatalf("pick: %q %v", out, err)
	}
	if got, _ := st.PickedSHAs(card.ID, repo); len(got) != 1 || got[0] != sha1 {
		t.Fatalf("recorded picks = %v", got)
	}
	if _, err := runCLI(env, "", "git", repo, "cherry-pick", "-x", sha1); err == nil {
		t.Error("a picked commit must not be picked twice")
	}
	if p, _ := runCLI(env, "", "pending-picks", repo); !strings.Contains(p, sha2) || strings.Contains(p, sha1) {
		t.Errorf("pending after pick = %q", p)
	}
	if b, _ := runCLI(env, "", "branches"); !strings.Contains(b, "mode cherry-pick") || !strings.Contains(b, "1 feature commits not yet picked") {
		t.Errorf("branches = %q", b)
	}
}

func TestStashDecisionAndPushGate(t *testing.T) {
	st, env, repo, card := branchFixture(t)
	f := "feature/" + card.Ident
	g(t, repo, "branch", "release")
	runCLI(env, "", "branch-plan", repo, "release")
	runCLI(env, "", "git", repo, "checkout", "-b", f, "release")
	os.WriteFile(filepath.Join(repo, "w.txt"), []byte("w"), 0o644)
	if _, err := runCLI(env, "", "git", repo, "stash", "push", "-u", "-m", card.Ident+" wip"); err != nil {
		t.Fatal(err)
	}
	if l, _ := st.ListStashes(card.ID); len(l) != 1 || l[0].Branch != f || l[0].PoppedAt != "" {
		t.Fatalf("stashes = %+v", l)
	}
	if _, err := runCLI(env, "", "git", repo, "stash", "pop"); err != nil {
		t.Fatal(err)
	}
	if l, _ := st.ListStashes(card.ID); l[0].PoppedAt == "" {
		t.Error("pop must be recorded")
	}
	// decisions
	if _, err := runCLI(env, "Release branch is release", "decision"); err != nil {
		t.Fatal(err)
	}
	if out, _ := runCLI(env, "", "decisions"); !strings.Contains(out, "Release branch is release") || !strings.Contains(out, "(claude)") {
		t.Errorf("decisions = %q", out)
	}
	if _, err := runCLI(env, "", "decision", "extra"); err == nil {
		t.Error("decision takes text on stdin only")
	}
	// push gate: -dev push refused while the repo is not integrated
	g(t, repo, "branch", "dev")
	runCLI(env, "", "git", repo, "checkout", "dev")
	runCLI(env, "", "git", repo, "checkout", "-b", f+"-dev", "dev")
	if _, err := runCLI(env, "", "git", repo, "push", "-u", "origin", f+"-dev"); err == nil || !strings.Contains(err.Error(), "nothing is pushed") {
		t.Fatalf("push gate: %v", err)
	}
	if out, err := runCLI(env, "", "remote-kind", repo); err != nil || strings.TrimSpace(out) != "none" {
		t.Errorf("remote-kind = %q %v", out, err)
	}
}
