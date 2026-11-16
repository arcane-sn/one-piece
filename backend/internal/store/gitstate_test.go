package store

import (
	"errors"
	"strings"
	"testing"
)

func TestModePicksAndPushReady(t *testing.T) {
	s, b, c, repo := repoBoard(t)
	br, _ := s.PlanBranch(c.ID, repo, "release")
	if br.Mode != "merge" || br.Picked != 0 {
		t.Fatalf("defaults = %+v", br)
	}
	cp, bad := "cherry-pick", "squash"
	if br, err := s.UpdateBranch(c.ID, repo, BranchPatch{Mode: &cp}); err != nil || br.Mode != "cherry-pick" {
		t.Fatalf("mode: %+v %v", br, err)
	}
	if _, err := s.UpdateBranch(c.ID, repo, BranchPatch{Mode: &bad}); !errors.As(err, &ValidationError{}) {
		t.Fatalf("bad mode: %v", err)
	}
	if ok, _ := s.PushReady(c.ID); ok {
		t.Error("a planned repo is not push-ready")
	}
	s.AddPicks(c.ID, repo, []string{"a1", "b2", "a1"})
	if got, _ := s.PickedSHAs(c.ID, repo); len(got) != 2 {
		t.Errorf("picks = %v", got)
	}
	if rows, _ := s.ListBranches(c.ID); rows[0].Picked != 2 {
		t.Errorf("picked count = %d", rows[0].Picked)
	}
	for _, st := range []string{"merged_dev", "pushed", "pr_open"} {
		s.UpdateBranch(c.ID, repo, BranchPatch{Status: &st})
		if ok, _ := s.PushReady(c.ID); !ok {
			t.Errorf("%s should be push-ready", st)
		}
	}
	// a second repo that is not integrated, or blocked, closes the gate
	repo2 := repo + "2"
	s.DB.Exec(`INSERT INTO card_branches(card_id,repo,base_branch,feature_branch,dev_branch,status) VALUES(?,?,?,?,?,?)`, c.ID, repo2, "r", "f", "f-dev", "branched")
	if ok, _ := s.PushReady(c.ID); ok {
		t.Error("an unintegrated repo must close the gate")
	}
	s.DB.Exec(`UPDATE card_branches SET status='blocked' WHERE repo=?`, repo2)
	if ok, _ := s.PushReady(c.ID); ok {
		t.Error("a blocked repo must close the gate")
	}
	// new commits after integration reopen the work
	s.RegressBranch(c.ID, repo)
	if rows, _ := s.ListBranches(c.ID); rows[0].Status != "branched" || rows[0].Note == "" {
		t.Errorf("regress = %+v", rows[0])
	}
	// deleting the row drops its picks
	s.DeleteBranch(c.ID, repo)
	if got, _ := s.PickedSHAs(c.ID, repo); len(got) != 0 {
		t.Errorf("picks after delete = %v", got)
	}
	_ = b
}

func TestStashRecords(t *testing.T) {
	s, _, c, repo := repoBoard(t)
	s.AddStash(c.ID, repo, "sha1", "feature/MID-1", "MID-1 wip")
	s.AddStash(c.ID, repo, "sha2", "feature/MID-1", "MID-1 more")
	s.MarkStashPopped(c.ID, repo, "sha2")
	l, _ := s.ListStashes(c.ID)
	if len(l) != 2 || l[0].SHA != "sha2" || l[0].PoppedAt == "" || l[1].PoppedAt != "" {
		t.Fatalf("stashes = %+v", l)
	}
}

func TestDecisionLog(t *testing.T) {
	s, _, c, _ := repoBoard(t)
	if _, err := s.AddDecision(c.ID, "  ", "claude"); !errors.As(err, &ValidationError{}) {
		t.Errorf("empty: %v", err)
	}
	if _, err := s.AddDecision(c.ID, strings.Repeat("x", 2001), "claude"); err == nil {
		t.Error("too long")
	}
	d, err := s.AddDecision(c.ID, "Release branch is release/2.0", "claude")
	if err != nil || d.CreatedAt == "" {
		t.Fatalf("add: %+v %v", d, err)
	}
	s.AddDecision(c.ID, "Use cherry-pick for api", "owner")
	if blk := DecisionBlock(mustList(t, s, c.ID)); !strings.Contains(blk, "release/2.0") || !strings.Contains(blk, "(owner)") {
		t.Errorf("block = %q", blk)
	}
	if DecisionBlock(nil) != "" {
		t.Error("empty log has no block")
	}
	if err := s.DeleteDecision(c.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDecision(c.ID, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
	for i := 0; i < 199; i++ {
		s.AddDecision(c.ID, "n", "claude")
	}
	if _, err := s.AddDecision(c.ID, "one more", "claude"); !errors.As(err, &ConflictError{}) {
		t.Errorf("201st: %v", err)
	}
	// only the newest 30 are shown, with a count of the omitted ones
	if blk := DecisionBlock(mustList(t, s, c.ID)); !strings.Contains(blk, "170 older notes omitted") {
		t.Errorf("block head = %q", blk[:80])
	}
	// cascade
	s.DeleteCard(c.ID)
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM card_decisions`).Scan(&n)
	if n != 0 {
		t.Errorf("decisions after card delete = %d", n)
	}
}

func mustList(t *testing.T, s *Store, id int64) []Decision {
	t.Helper()
	l, err := s.ListDecisions(id)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
