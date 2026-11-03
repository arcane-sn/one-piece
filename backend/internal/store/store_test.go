package store

import (
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	return s
}

func TestOpenIsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	for i := 0; i < 2; i++ {
		s, err := Open(p)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		s.DB.Close()
	}
}

func TestDeletingBoardCascades(t *testing.T) {
	s := openTemp(t)
	s.DB.Exec(`INSERT INTO boards(id,name) VALUES(1,'b')`)
	s.DB.Exec(`INSERT INTO board_folders(board_id,path,position) VALUES(1,'/x',0)`)
	s.DB.Exec(`INSERT INTO cards(board_id,title,description,status,position) VALUES(1,'c','','business',0)`)
	if _, err := s.DB.Exec(`DELETE FROM boards WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"board_folders", "cards"} {
		var n int
		s.DB.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n)
		if n != 0 {
			t.Errorf("%s has %d rows after board delete", tbl, n)
		}
	}
}
