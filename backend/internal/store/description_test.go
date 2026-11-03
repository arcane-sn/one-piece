package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBoardDescriptionRoundTrip(t *testing.T) {
	s := openTemp(t)
	b, err := s.CreateBoard("Shop", "  Sells things  ", nil)
	if err != nil || b.Description != "Sells things" {
		t.Fatalf("create = %+v, %v", b, err)
	}
	got, _ := s.GetBoard(b.ID)
	list, _ := s.ListBoards()
	if got.Description != "Sells things" || list[0].Description != "Sells things" {
		t.Fatalf("get=%q list=%q", got.Description, list[0].Description)
	}
	upd, _ := s.UpdateBoard(b.ID, "Shop", "New text", nil)
	if upd.Description != "New text" {
		t.Fatalf("update = %q", upd.Description)
	}
	if _, err := s.CreateBoard("x", strings.Repeat("a", 501), nil); !isValidation(err) {
		t.Fatalf("500+ char description should be rejected, got %v", err)
	}
}

// An existing database from slice 1 has no description column yet.
func TestOpenMigratesBoardsWithoutDescription(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// rebuild the pre-description boards table (throwaway temp DB)
	for _, q := range []string{
		`DROP TABLE cards`, `DROP TABLE board_folders`, `DROP TABLE boards`,
		`CREATE TABLE boards(id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`INSERT INTO boards(name) VALUES('legacy')`,
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
	var d string
	if err := s.DB.QueryRow(`SELECT description FROM boards WHERE name='legacy'`).Scan(&d); err != nil || d != "" {
		t.Fatalf("description = %q, %v", d, err)
	}
}
