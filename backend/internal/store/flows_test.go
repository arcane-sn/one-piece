package store

import "testing"

func TestFlowsCRUDBumpsCardVersionAndRevokesApproval(t *testing.T) {
	s, c := newCard(t)
	f, err := s.CreateFlow(c.ID, "Login", "A -> B: hi")
	if err != nil || f.Title != "Login" || f.Source != "A -> B: hi" || f.CardID != c.ID {
		t.Fatalf("create = %+v, %v", f, err)
	}
	if got, _ := s.GetCard(c.ID); got.ContentVersion != 2 {
		t.Fatalf("adding a flow should bump version to 2, got %d", got.ContentVersion)
	}
	s.DB.Exec(`UPDATE cards SET approved=1 WHERE id=?`, c.ID)
	f, err = s.UpdateFlow(f.ID, "Login v2", "A -> B: yo")
	if err != nil || f.Title != "Login v2" {
		t.Fatalf("update = %+v, %v", f, err)
	}
	got, _ := s.GetCard(c.ID)
	if got.ContentVersion != 3 || got.Approved {
		t.Fatalf("editing a flow must bump version and revoke approval: %+v", got)
	}
	s.CreateFlow(c.ID, "Second", "")
	fs, _ := s.ListFlows(c.ID)
	if len(fs) != 2 || fs[0].Title != "Login v2" || fs[1].Title != "Second" {
		t.Fatalf("list = %+v", fs)
	}
	if err := s.DeleteFlow(f.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetCard(c.ID); got.ContentVersion != 5 {
		t.Fatalf("delete should bump version, got %d", got.ContentVersion)
	}
	if fs, _ = s.ListFlows(c.ID); len(fs) != 1 {
		t.Fatalf("after delete: %+v", fs)
	}
}

func TestFlowValidationAndMissing(t *testing.T) {
	s, c := newCard(t)
	if _, err := s.CreateFlow(c.ID, "  ", "x"); err == nil {
		t.Fatal("empty title should fail")
	}
	if _, err := s.CreateFlow(c.ID, "ok", string(make([]rune, 50001))); err == nil {
		t.Fatal("oversized source should fail")
	}
	if _, err := s.CreateFlow(9999, "ok", ""); err != ErrNotFound {
		t.Fatalf("missing card: %v", err)
	}
	if _, err := s.UpdateFlow(9999, "ok", ""); err != ErrNotFound {
		t.Fatalf("missing flow update: %v", err)
	}
	if err := s.DeleteFlow(9999); err != ErrNotFound {
		t.Fatalf("missing flow delete: %v", err)
	}
}
