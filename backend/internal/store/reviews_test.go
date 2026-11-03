package store

import (
	"errors"
	"reflect"
	"testing"
)

func newCard(t *testing.T) (*Store, Card) {
	t.Helper()
	s := openTemp(t)
	b, _ := s.CreateBoard("b", "", nil)
	c, err := s.CreateCard(b.ID, "Title", "Body", "")
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

func isConflict(err error) bool { var ce ConflictError; return errors.As(err, &ce) }

func TestContentVersionBumpsOnlyOnContentChange(t *testing.T) {
	s, c := newCard(t)
	if c.ContentVersion != 1 || c.Approved {
		t.Fatalf("new card = %+v", c)
	}
	u, _ := s.UpdateCard(c.ID, "Title", "Body", "2026-12-31") // deadline only
	if u.ContentVersion != 1 {
		t.Fatalf("deadline change must not bump version, got %d", u.ContentVersion)
	}
	u, _ = s.UpdateCard(c.ID, "Title 2", "Body", "2026-12-31")
	if u.ContentVersion != 2 {
		t.Fatalf("title change should bump to 2, got %d", u.ContentVersion)
	}
	u, _ = s.UpdateCard(c.ID, "Title 2", "Body 2", "2026-12-31")
	if u.ContentVersion != 3 {
		t.Fatalf("description change should bump to 3, got %d", u.ContentVersion)
	}
}

func TestReviewLifecycle(t *testing.T) {
	s, c := newCard(t)
	if _, err := s.LatestReview(c.ID); err != ErrNotFound {
		t.Fatalf("no review yet: %v", err)
	}
	r, err := s.StartReview(c.ID, c.ContentVersion)
	if err != nil || r.Status != "running" {
		t.Fatalf("start = %+v, %v", r, err)
	}
	s.AppendReviewLog(r.ID, "Reading a.go")
	s.AppendReviewLog(r.ID, "Claude: thinking")
	if err := s.FinishReview(r.ID, "needs_work", "Too vague", []string{"No DoD", "Scope too big"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LatestReview(c.ID)
	if got.Status != "done" || got.Verdict != "needs_work" || got.Summary != "Too vague" ||
		!reflect.DeepEqual(got.Missing, []string{"No DoD", "Scope too big"}) ||
		!reflect.DeepEqual(got.Log, []string{"Reading a.go", "Claude: thinking"}) || got.ContentVersion != 1 {
		t.Fatalf("latest = %+v", got)
	}
	r2, _ := s.StartReview(c.ID, 2)
	if l, _ := s.LatestReview(c.ID); l.ID != r2.ID {
		t.Fatalf("latest should be the newest review")
	}
}

func TestFailRunningReviewsAndCascade(t *testing.T) {
	s, c := newCard(t)
	r, _ := s.StartReview(c.ID, 1)
	if err := s.FailRunningReviews(); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LatestReview(c.ID)
	if got.ID != r.ID || got.Status != "failed" || got.Error == "" {
		t.Fatalf("after restart sweep = %+v", got)
	}
	if err := s.DeleteCard(c.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM card_reviews`).Scan(&n)
	if n != 0 {
		t.Fatalf("reviews should be deleted with the card, %d left", n)
	}
}

func TestApprovalGate(t *testing.T) {
	s, c := newCard(t)
	if _, err := s.SetApproved(c.ID, true); !isConflict(err) {
		t.Fatalf("no review: want conflict, got %v", err)
	}
	r, _ := s.StartReview(c.ID, 1)
	if _, err := s.SetApproved(c.ID, true); !isConflict(err) {
		t.Fatalf("running review: want conflict, got %v", err)
	}
	s.FinishReview(r.ID, "needs_work", "x", []string{"y"})
	if _, err := s.SetApproved(c.ID, true); !isConflict(err) {
		t.Fatalf("needs_work: want conflict, got %v", err)
	}
	r2, _ := s.StartReview(c.ID, 1)
	s.FinishReview(r2.ID, "pass", "good", nil)
	got, err := s.SetApproved(c.ID, true)
	if err != nil || !got.Approved {
		t.Fatalf("pass: %+v, %v", got, err)
	}
	// editing the ticket revokes approval and makes the old review stale
	u, _ := s.UpdateCard(c.ID, "Changed", "Body", "")
	if u.Approved {
		t.Fatal("content edit must revoke approval")
	}
	if _, err := s.SetApproved(c.ID, true); !isConflict(err) {
		t.Fatalf("stale review: want conflict, got %v", err)
	}
	if got, err := s.SetApproved(c.ID, false); err != nil || got.Approved {
		t.Fatalf("unapprove always allowed: %+v, %v", got, err)
	}
	if _, err := s.SetApproved(9999, true); err != ErrNotFound {
		t.Fatalf("unknown card: %v", err)
	}
}
