package store

import (
	"errors"
	"path/filepath"
	"regexp"
	"testing"
)

var keyRe = regexp.MustCompile(`^[A-Z]{2,5}$`)

func TestDeriveKey(t *testing.T) {
	cases := []struct {
		name  string
		taken map[string]bool
		want  string
	}{
		{"Midas", nil, "MID"},
		{"Midas 2", map[string]bool{"MID": true}, "MIDA"},
		{"2024!!", nil, "BRD"},
		{"日本", nil, "BRD"},
	}
	for _, c := range cases {
		if got := DeriveKey(c.name, c.taken); got != c.want {
			t.Errorf("DeriveKey(%q) = %q, want %q", c.name, got, c.want)
		}
	}
	taken := map[string]bool{"BRD": true}
	got := DeriveKey("2024", taken)
	if taken[got] || !keyRe.MatchString(got) {
		t.Errorf("BRD taken: got %q", got)
	}
	// exhaust every 3-5 letter prefix of "Midas" and keep going
	taken = map[string]bool{"MID": true, "MIDA": true, "MIDAS": true}
	got = DeriveKey("Midas", taken)
	if taken[got] || !keyRe.MatchString(got) {
		t.Errorf("all prefixes taken: got %q", got)
	}
}

func TestCardIdentStableAndNeverReused(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("Midas", "", nil)
	if b.Key != "MID" {
		t.Fatalf("key = %q", b.Key)
	}
	c1, _ := s.CreateCard(b.ID, "One", "", "")
	c2, _ := s.CreateDraft(b.ID, "Two", "")
	if c1.Ident != "MID-1" || c2.Ident != "MID-2" {
		t.Fatalf("idents = %q %q", c1.Ident, c2.Ident)
	}
	if err := s.DeleteCard(c1.ID); err != nil {
		t.Fatal(err)
	}
	c3, _ := s.CreateCard(b.ID, "Three", "", "")
	if c3.Ident != "MID-3" {
		t.Fatalf("after delete = %q", c3.Ident)
	}
	nb, err := s.SetBoardKey(b.ID, "XYZ")
	if err != nil || nb.Key != "XYZ" {
		t.Fatalf("SetBoardKey = %+v, %v", nb, err)
	}
	if g, _ := s.GetCard(c2.ID); g.Ident != "MID-2" {
		t.Fatalf("existing ident changed: %q", g.Ident)
	}
	c4, _ := s.CreateCard(b.ID, "Four", "", "")
	if c4.Ident != "XYZ-4" {
		t.Fatalf("after rekey = %q", c4.Ident)
	}
}

func TestSetBoardKeyValidation(t *testing.T) {
	s := openTemp(t)
	a, _ := s.CreateBoard("Alpha", "", nil)
	b, _ := s.CreateBoard("Beta", "", nil)
	for _, k := range []string{"a", "TOOLONG", "mid", "M1", ""} {
		if _, err := s.SetBoardKey(a.ID, k); !errors.As(err, &ValidationError{}) {
			t.Errorf("key %q: err = %v", k, err)
		}
	}
	if _, err := s.SetBoardKey(a.ID, b.Key); !errors.As(err, &ValidationError{}) {
		t.Errorf("duplicate key: err = %v", err)
	}
	if _, err := s.SetBoardKey(a.ID, a.Key); err != nil {
		t.Errorf("own key must be allowed: %v", err)
	}
}

func TestItemIdentsNeverReused(t *testing.T) {
	s, c := newCard(t)
	t1, _ := s.CreateItem(c.ID, "agent", "a", "")
	t2, _ := s.CreateItem(c.ID, "agent", "b", "")
	q1, _ := s.CreateItem(c.ID, "qa", "q", "")
	if t1.Ident != c.Ident+"-T1" || t2.Ident != c.Ident+"-T2" || q1.Ident != c.Ident+"-QA1" {
		t.Fatalf("idents = %q %q %q", t1.Ident, t2.Ident, q1.Ident)
	}
	s.DeleteItem(t2.ID)
	t3, _ := s.CreateItem(c.ID, "agent", "c", "")
	if t3.Ident != c.Ident+"-T3" {
		t.Fatalf("after delete = %q", t3.Ident)
	}
	fix, ok, err := s.AddFixTask(q1, "i", "d")
	if err != nil || !ok || fix.Ident != c.Ident+"-T4" {
		t.Fatalf("fix = %+v %v %v", fix, ok, err)
	}
	out, err := s.ReplaceItemsOfKind(c.ID, "agent", []Item{{Title: "x"}, {Title: "y"}})
	if err != nil || out[0].Ident != c.Ident+"-T5" || out[1].Ident != c.Ident+"-T6" {
		t.Fatalf("replace = %+v %v", out, err)
	}
	all, _ := s.ListItems(c.ID)
	found := false
	for _, it := range all {
		if it.ID == fix.ID {
			found = it.Ident == c.Ident+"-T4"
		}
	}
	if !found {
		t.Fatal("[FIX] row must be kept with its ident")
	}
	q2, _ := s.CreateItem(c.ID, "qa", "q2", "")
	if q2.Ident != c.Ident+"-QA2" {
		t.Fatalf("qa counter = %q", q2.Ident)
	}
}

func TestBackfillIdents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO boards(id,name) VALUES(1,'Midas'),(2,'Orbit')`,
		`INSERT INTO cards(id,board_id,title,status,position) VALUES(1,1,'a','business',0),(2,1,'b','business',1),(3,2,'c','draft',0)`,
		`INSERT INTO card_items(id,card_id,kind,title) VALUES(1,1,'agent','x'),(2,1,'qa','y'),(3,1,'agent','z')`,
	} {
		if _, err := s.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.DB.Close()
	s, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	for id, want := range map[int64]string{1: "MID-1", 2: "MID-2", 3: "ORB-1"} {
		if c, _ := s.GetCard(id); c.Ident != want {
			t.Errorf("card %d ident = %q, want %q", id, c.Ident, want)
		}
	}
	for id, want := range map[int64]string{1: "MID-1-T1", 2: "MID-1-QA1", 3: "MID-1-T2"} {
		if it, _ := s.GetItem(id); it.Ident != want {
			t.Errorf("item %d ident = %q, want %q", id, it.Ident, want)
		}
	}
	c, _ := s.CreateCard(1, "next", "", "")
	it, _ := s.CreateItem(1, "agent", "n", "")
	if c.Ident != "MID-3" || it.Ident != "MID-1-T3" {
		t.Fatalf("continue = %q %q", c.Ident, it.Ident)
	}
}
