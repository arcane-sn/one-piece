package store

import (
	"strings"
	"testing"
)

func TestListCardsOrdersDraftFirst(t *testing.T) {
	s, bid, cs := threeCards(t)
	if _, err := s.MoveCard(cs[2].ID, "draft", 0); err != nil { // "c" goes to Draft
		t.Fatal(err)
	}
	all, _ := s.ListCards(bid)
	if len(all) != 3 || all[0].Title != "c" || all[0].Status != "draft" {
		t.Fatalf("draft should list first: %+v", all)
	}
}

func TestMoveFromDraftOnlyToBusiness(t *testing.T) {
	s, _, cs := threeCards(t)
	s.MoveCard(cs[0].ID, "draft", 0)
	for _, to := range []string{"agent", "qa", "done"} {
		if _, err := s.MoveCard(cs[0].ID, to, 0); !isConflict(err) {
			t.Errorf("draft -> %s: want ConflictError, got %v", to, err)
		}
	}
	if c, err := s.MoveCard(cs[0].ID, "business", 0); err != nil || c.Status != "business" {
		t.Errorf("draft -> business = %+v, %v", c, err)
	}
}

func TestMoveToDraftOnlyFromBusiness(t *testing.T) {
	s, _, cs := threeCards(t)
	s.MoveCard(cs[0].ID, "agent", 0)
	if _, err := s.MoveCard(cs[0].ID, "draft", 0); !isConflict(err) {
		t.Fatalf("agent -> draft: want ConflictError, got %v", err)
	}
	// A business card keeps its review but loses approval when parked in Draft.
	r, _ := s.StartReview(cs[1].ID, 1)
	s.FinishReview(r.ID, "pass", "ok", nil)
	s.DB.Exec(`UPDATE cards SET approved=1 WHERE id=?`, cs[1].ID)
	c, err := s.MoveCard(cs[1].ID, "draft", 0)
	if err != nil || c.Status != "draft" || c.Approved {
		t.Fatalf("business -> draft = %+v, %v", c, err)
	}
	if _, err := s.LatestReview(cs[1].ID); err != nil {
		t.Errorf("review must be kept: %v", err)
	}
}

func TestMoveToDraftBlockedWhileReviewRuns(t *testing.T) {
	s, _, cs := threeCards(t)
	s.StartReview(cs[0].ID, 1) // still running
	if _, err := s.MoveCard(cs[0].ID, "draft", 0); !isConflict(err) {
		t.Fatalf("want ConflictError, got %v", err)
	}
	if got, _ := s.GetCard(cs[0].ID); got.Status != "business" {
		t.Errorf("card must stay in business, got %q", got.Status)
	}
}

func TestSetApprovedRejectsDraft(t *testing.T) {
	s, _, cs := threeCards(t)
	s.MoveCard(cs[0].ID, "draft", 0)
	_, err := s.SetApproved(cs[0].ID, true)
	if !isConflict(err) || !strings.Contains(err.Error(), "Draft") {
		t.Fatalf("want the Draft ConflictError, got %v", err)
	}
}
