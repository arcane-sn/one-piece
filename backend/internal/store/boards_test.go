package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFolderRejectsRelativePath(t *testing.T) {
	s := openTemp(t)
	if _, err := s.CreateBoard("b", []string{"relative/dir"}); !isValidation(err) {
		t.Fatalf("want ValidationError, got %v", err)
	}
}

func TestFolderRejectsMissingDir(t *testing.T) {
	s := openTemp(t)
	missing := filepath.Join(t.TempDir(), "nope")
	if _, err := s.CreateBoard("b", []string{missing}); !isValidation(err) {
		t.Fatalf("want ValidationError, got %v", err)
	}
}

func TestFolderRejectsFile(t *testing.T) {
	s := openTemp(t)
	f := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(f, []byte("x"), 0o644)
	if _, err := s.CreateBoard("b", []string{f}); !isValidation(err) {
		t.Fatalf("want ValidationError, got %v", err)
	}
}

func TestFolderNormalizesAndDedupes(t *testing.T) {
	s := openTemp(t)
	dir := t.TempDir()
	in := []string{dir, dir + "/", filepath.Join(dir, "..", filepath.Base(dir))}
	b, err := s.CreateBoard("b", in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.Folders, []string{dir}) {
		t.Errorf("folders = %v, want [%s]", b.Folders, dir)
	}
}

func TestBoardNameRejectsBlankAndTooLong(t *testing.T) {
	s := openTemp(t)
	for _, name := range []string{"", "   ", strings.Repeat("a", 101)} {
		if _, err := s.CreateBoard(name, nil); !isValidation(err) {
			t.Errorf("name %q: want ValidationError, got %v", name, err)
		}
	}
}

func TestBoardCountsByStatus(t *testing.T) {
	s := openTemp(t)
	b, err := s.CreateBoard("b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.Counts, map[string]int{"business": 0, "agent": 0, "qa": 0, "done": 0}) {
		t.Errorf("fresh board counts = %v", b.Counts)
	}
	var ids []int64
	for _, title := range []string{"a", "b", "c"} {
		c, _ := s.CreateCard(b.ID, title, "")
		ids = append(ids, c.ID)
	}
	s.MoveCard(ids[0], "agent", 0)
	want := map[string]int{"business": 2, "agent": 1, "qa": 0, "done": 0}
	list, err := s.ListBoards()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(list[0].Counts, want) {
		t.Errorf("ListBoards counts = %v, want %v", list[0].Counts, want)
	}
	got, _ := s.GetBoard(b.ID)
	if !reflect.DeepEqual(got.Counts, want) {
		t.Errorf("GetBoard counts = %v, want %v", got.Counts, want)
	}
	upd, _ := s.UpdateBoard(b.ID, "renamed", nil)
	if !reflect.DeepEqual(upd.Counts, want) {
		t.Errorf("UpdateBoard counts = %v, want %v", upd.Counts, want)
	}
}

func TestDeleteBoardRemovesCards(t *testing.T) {
	s := openTemp(t)
	b, err := s.CreateBoard("b", nil)
	if err != nil {
		t.Fatal(err)
	}
	s.DB.Exec(`INSERT INTO cards(board_id,title,description,status,position) VALUES(?,?,?,?,?)`, b.ID, "c", "", "business", 0)
	if err := s.DeleteBoard(b.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM cards`).Scan(&n)
	if n != 0 {
		t.Errorf("%d cards left", n)
	}
	if err := s.DeleteBoard(b.ID); err != ErrNotFound {
		t.Errorf("second delete: %v, want ErrNotFound", err)
	}
}
