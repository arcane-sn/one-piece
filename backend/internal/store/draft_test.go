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

func TestCreateDraftLandsInDraftAtEnd(t *testing.T) {
	s, bid := newBoard(t)
	a, err := s.CreateDraft(bid, "A", "first")
	if err != nil || a.Status != "draft" || a.Position != 0 || a.Description != "first" {
		t.Fatalf("a = %+v, %v", a, err)
	}
	if b, _ := s.CreateDraft(bid, "B", ""); b.Position != 1 {
		t.Fatalf("second draft should be at position 1: %+v", b)
	}
	if _, err := s.CreateDraft(bid, "  ", ""); !isValidation(err) {
		t.Fatalf("blank title: want ValidationError, got %v", err)
	}
}

func TestUpdateDraftRejectsNonDraftAndOtherBoard(t *testing.T) {
	s, bid := newBoard(t)
	other, _ := s.CreateBoard("other", "", nil)
	biz, _ := s.CreateCard(bid, "Biz", "", "")
	if _, err := s.UpdateDraft(bid, biz.ID, "x", ""); !isConflict(err) {
		t.Errorf("non-draft card: want ConflictError, got %v", err)
	}
	d, _ := s.CreateDraft(bid, "D", "old")
	if _, err := s.UpdateDraft(other.ID, d.ID, "x", ""); !isConflict(err) {
		t.Errorf("other board's draft: want ConflictError, got %v", err)
	}
	u, err := s.UpdateDraft(bid, d.ID, "D2", "new")
	if err != nil || u.Title != "D2" || u.Description != "new" || u.ContentVersion != d.ContentVersion+1 {
		t.Errorf("own draft update = %+v, %v", u, err)
	}
	if got, _ := s.GetCard(biz.ID); got.Title != "Biz" {
		t.Errorf("business card must stay untouched, got %+v", got)
	}
}

func TestTicketIndexLines(t *testing.T) {
	if got := TicketIndex(nil); got != "" {
		t.Fatalf("empty index = %q", got)
	}
	got := TicketIndex([]Card{{ID: 1, Status: "draft", Title: "A"}, {ID: 2, Status: "business", Title: "B"}})
	if got != "#1 draft A\n#2 business B" {
		t.Fatalf("index = %q", got)
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
