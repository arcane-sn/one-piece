package store

import "testing"

func TestQAEvidenceStates(t *testing.T) {
	s, _, cards := threeCards(t)
	c := cards[0]
	it, err := s.CreateItem(c.ID, "qa", "Login works", "")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListItems(c.ID)
	if list[0].Evidence != nil {
		t.Fatal("never run must have no evidence")
	}
	rid, _ := s.CreateQARun(it.ID, c.ID, c.BoardID, nil)
	s.AddQASteps(rid, 1, nil)
	s.FinishQARun(rid, "passed", "", "", "ok", 1, nil)
	list, _ = s.ListItems(c.ID)
	if e := list[0].Evidence; e == nil || e.State != "passed" || e.Runs != 1 {
		t.Fatalf("evidence = %+v", e)
	}
	s.DB.Exec(`UPDATE cards SET content_version=content_version+1 WHERE id=?`, c.ID)
	list, _ = s.ListItems(c.ID)
	if list[0].Evidence.State != "stale" {
		t.Errorf("a ticket change after a pass must make it stale, got %q", list[0].Evidence.State)
	}
	cs, _ := s.ListCards(c.BoardID)
	for _, x := range cs {
		if x.ID == c.ID && (x.QATotal != 1 || x.QARan != 1) {
			t.Errorf("card qa counts = %d/%d", x.QARan, x.QATotal)
		}
	}
}

func TestCardUpdatedAtMovesOnRealChangesOnly(t *testing.T) {
	s, _, cards := threeCards(t)
	var before string
	s.DB.QueryRow(`SELECT updated_at FROM cards WHERE id=?`, cards[1].ID).Scan(&before)
	s.DB.Exec(`UPDATE cards SET updated_at='2000-01-01T00:00:00.000Z' WHERE id=?`, cards[1].ID)
	s.DB.Exec(`UPDATE cards SET position=position+5 WHERE id=?`, cards[1].ID) // renumbering is not an update
	var got string
	s.DB.QueryRow(`SELECT updated_at FROM cards WHERE id=?`, cards[1].ID).Scan(&got)
	if got != "2000-01-01T00:00:00.000Z" {
		t.Fatalf("position change touched updated_at: %q", got)
	}
	if _, err := s.UpdateCard(cards[1].ID, "New title", "", ""); err != nil {
		t.Fatal(err)
	}
	s.DB.QueryRow(`SELECT updated_at FROM cards WHERE id=?`, cards[1].ID).Scan(&got)
	if got <= "2000-01-01T00:00:00.000Z" {
		t.Errorf("a title change must touch updated_at, got %q", got)
	}
	_ = before
}
