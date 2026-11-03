package store

import (
	"path/filepath"
	"testing"
)

func TestDeadlineRoundTrip(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("b", "", nil)
	c, err := s.CreateCard(b.ID, "t", "", "2026-12-31")
	if err != nil || c.Deadline != "2026-12-31" {
		t.Fatalf("create = %+v, %v", c, err)
	}
	list, _ := s.ListCards(b.ID)
	if list[0].Deadline != "2026-12-31" {
		t.Fatalf("list deadline = %q", list[0].Deadline)
	}
	upd, err := s.UpdateCard(c.ID, "t", "", "")
	if err != nil || upd.Deadline != "" {
		t.Fatalf("clearing deadline: %+v, %v", upd, err)
	}
}

func TestDeadlineRejectsBadValues(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("b", "", nil)
	c, _ := s.CreateCard(b.ID, "t", "", "")
	for _, bad := range []string{"tomorrow", "2026-13-45", "2026-1-5", "31/12/2026"} {
		if _, err := s.CreateCard(b.ID, "x", "", bad); !isValidation(err) {
			t.Errorf("create with %q: want validation error, got %v", bad, err)
		}
		if _, err := s.UpdateCard(c.ID, "t", "", bad); !isValidation(err) {
			t.Errorf("update with %q: want validation error, got %v", bad, err)
		}
	}
}

// A database from before deadlines existed has a cards table without the column.
func TestOpenMigratesCardsWithoutDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// rebuild the pre-deadline cards table (throwaway temp DB)
	for _, q := range []string{
		`DROP TABLE cards`,
		`CREATE TABLE cards(id INTEGER PRIMARY KEY, board_id INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
		  title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, position INTEGER NOT NULL)`,
		`INSERT INTO boards(name) VALUES('b')`,
		`INSERT INTO cards(board_id,title,status,position) VALUES(1,'legacy','business',0)`,
	} {
		if _, err := old.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.DB.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.DB.Close()
	cs, err := s.ListCards(1)
	if err != nil || len(cs) != 1 || cs[0].Deadline != "" {
		t.Fatalf("cards = %+v, %v", cs, err)
	}
}
