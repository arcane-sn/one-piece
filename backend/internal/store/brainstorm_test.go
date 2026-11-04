package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func newBoard(t *testing.T) (*Store, int64) {
	t.Helper()
	s := openTemp(t)
	b, err := s.CreateBoard("b", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, b.ID
}

func TestPlanVersionsIncrementAndRestoreKeepsHistory(t *testing.T) {
	s, bid := newBoard(t)
	if _, err := s.LatestPlanVersion(bid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no plan yet: want ErrNotFound, got %v", err)
	}
	for _, c := range []string{"one", "two", "three"} {
		if _, err := s.AddPlanVersion(bid, c); err != nil {
			t.Fatal(err)
		}
	}
	vs, _ := s.ListPlanVersions(bid)
	if len(vs) != 3 || vs[0].Version != 3 || vs[2].Version != 1 || vs[0].Content != "" {
		t.Fatalf("list = %+v", vs)
	}
	if v, err := s.GetPlanVersion(bid, 1); err != nil || v.Content != "one" {
		t.Fatalf("get v1 = %+v, %v", v, err)
	}
	if v, _ := s.LatestPlanVersion(bid); v.Version != 3 || v.Content != "three" {
		t.Fatalf("latest = %+v", v)
	}
	if _, err := s.GetPlanVersion(bid, 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing version: want ErrNotFound, got %v", err)
	}
}

func TestBrainstormTextAppendAndStatus(t *testing.T) {
	s, bid := newBoard(t)
	if _, err := s.AddBrainstormMessage(bid, "user", "hello", "done"); err != nil {
		t.Fatal(err)
	}
	m, _ := s.AddBrainstormMessage(bid, "assistant", "", "running")
	s.AppendBrainstormText(m.ID, "first")
	s.AppendBrainstormText(m.ID, "second")
	if running, _ := s.RunningBrainstormTurn(bid); !running {
		t.Fatal("a running assistant message means a running turn")
	}
	s.FinishBrainstormMessage(m.ID, "done")
	if running, _ := s.RunningBrainstormTurn(bid); running {
		t.Fatal("turn should be finished")
	}
	ms, _ := s.ListBrainstormMessages(bid)
	if len(ms) != 2 || ms[0].Role != "user" || ms[1].Text != "first\n\nsecond" || ms[1].Status != "done" {
		t.Fatalf("messages = %+v", ms)
	}
}

func TestFailRunningBrainstormMarksStopped(t *testing.T) {
	s, bid := newBoard(t)
	m, _ := s.AddBrainstormMessage(bid, "assistant", "partial", "running")
	if err := s.FailRunningBrainstorm(); err != nil {
		t.Fatal(err)
	}
	ms, _ := s.ListBrainstormMessages(bid)
	if ms[0].ID != m.ID || ms[0].Status != "stopped" || ms[0].Text != "partial" {
		t.Fatalf("after sweep = %+v", ms[0])
	}
}

func TestBrainstormStateDefaultsToZero(t *testing.T) {
	s, bid := newBoard(t)
	if st, err := s.GetBrainstormState(bid); err != nil || st != (BrainstormState{}) {
		t.Fatalf("default = %+v, %v", st, err)
	}
	s.SetBrainstormState(bid, BrainstormState{SessionID: "sess-1", SentIndex: "#1 draft A"})
	s.SetBrainstormState(bid, BrainstormState{SessionID: "sess-2", SentIndex: "#1 draft A"}) // upsert
	if st, _ := s.GetBrainstormState(bid); st.SessionID != "sess-2" {
		t.Fatalf("state = %+v", st)
	}
}

func TestDeleteBoardCascadesBrainstormData(t *testing.T) {
	s, bid := newBoard(t)
	s.AddBrainstormMessage(bid, "user", "hi", "done")
	s.SetBrainstormState(bid, BrainstormState{SessionID: "x"})
	s.AddPlanVersion(bid, "plan")
	if err := s.DeleteBoard(bid); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"brainstorm_messages", "brainstorm_state", "plan_versions"} {
		var n int
		s.DB.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n)
		if n != 0 {
			t.Errorf("%s still has %d rows after board delete", tbl, n)
		}
	}
}

func TestOpenUsesWAL(t *testing.T) {
	s := openTemp(t)
	var mode string
	if err := s.DB.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v", mode, err)
	}
}

func TestStoreDirAndPathAreAbsoluteForARelativeDBName(t *testing.T) {
	t.Chdir(t.TempDir())
	s, err := Open("rel.db") // how start.sh runs it: a bare file name
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	if !filepath.IsAbs(s.Dir) || !filepath.IsAbs(s.Path) || filepath.Base(s.Path) != "rel.db" || !filepath.IsAbs(s.PlanPath(1)) {
		t.Fatalf("Dir=%q Path=%q PlanPath=%q must all be absolute", s.Dir, s.Path, s.PlanPath(1))
	}
}

func TestPlanPathLivesNextToTheDatabase(t *testing.T) {
	s := openTemp(t)
	want := filepath.Join(s.Dir, "plans", "board-7.md")
	if got := s.PlanPath(7); got != want || s.Dir == "" {
		t.Fatalf("PlanPath = %q, want %q", got, want)
	}
}
