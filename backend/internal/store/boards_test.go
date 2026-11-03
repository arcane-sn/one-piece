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
