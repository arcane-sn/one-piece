package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/qa"
)

func TestQAConfigRoundTripAndCascade(t *testing.T) {
	s := openTemp(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	os.MkdirAll(filepath.Join(root, "web"), 0o755)
	b, _ := s.CreateBoard("Midas", "", []string{root})
	if c, _ := s.GetQAConfig(b.ID); c.Configured() {
		t.Fatal("new board must not be configured")
	}
	cfg := qa.Config{BaseURL: "http://localhost:5000", AllowedHosts: []string{"Auth.Example.test"}, Headed: true,
		Services: []qa.Service{{Name: "web", Folder: filepath.Join(root, "web"), Command: "npm start", Port: 5000, ReadyURL: "http://localhost:5000/"}}}
	got, err := s.SetQAConfig(b.ID, cfg)
	if err != nil || !got.Configured() || got.AllowedHosts[0] != "auth.example.test" || !got.Headed || got.Services[0].ReadyTimeoutS != 120 || got.Services[0].ID == 0 {
		t.Fatalf("set: %+v %v", got, err)
	}
	bad := cfg
	bad.Services = []qa.Service{{Name: "web", Folder: t.TempDir(), Command: "x", Port: 5000, ReadyURL: "http://localhost:5000/"}}
	if _, err := s.SetQAConfig(b.ID, bad); !errors.As(err, &ValidationError{}) {
		t.Fatalf("outside folder: %v", err)
	}
	if c, _ := s.GetQAConfig(b.ID); !c.Configured() {
		t.Error("a rejected update must keep the old config")
	}
	if _, err := s.SetQAConfig(b.ID, qa.Config{}); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.GetQAConfig(b.ID); c.Configured() {
		t.Error("empty config removes the stack")
	}
}

func TestQARunsStepsRetentionCascade(t *testing.T) {
	s, b, c, _ := repoBoard(t)
	it, _ := s.CreateItem(c.ID, "qa", "Login", "steps")
	var first int64
	for i := 0; i < 12; i++ {
		id, err := s.CreateQARun(it.ID, c.ID, b.ID, []qa.RepoState{{Repo: "/r", Branch: "feature/MID-1", SHA: "abc"}})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = id
		}
		s.AddQASteps(id, 1, []qa.Step{{Position: 1, Title: "Open", Status: "passed", Screenshot: "steps/01-open.png"}})
		s.FinishQARun(id, "passed", "", "", "ok", 1, []string{"h.example.org"})
	}
	runs, _ := s.ListQARuns(it.ID)
	if len(runs) != 12 || runs[0].Steps[0].Title != "Open" || runs[0].Repos[0].SHA != "abc" || runs[0].BlockedHosts[0] != "h.example.org" || runs[0].FinishedAt == "" {
		t.Fatalf("runs = %+v", runs[0])
	}
	del, _ := s.PruneQARuns(it.ID)
	if len(del) != 2 || del[1] != first {
		t.Errorf("pruned = %v", del)
	}
	if runs, _ = s.ListQARuns(it.ID); len(runs) != 10 {
		t.Errorf("kept %d", len(runs))
	}
	if st := s.LastQAStatus(it.ID); st != "passed" {
		t.Errorf("last = %q", st)
	}
	id, _ := s.CreateQARun(it.ID, c.ID, b.ID, nil)
	s.StopRunningQARuns()
	if r, _ := s.GetQARun(id); r.Status != "stopped" {
		t.Errorf("running run after restart = %q", r.Status)
	}
	s.DeleteCard(c.ID)
	if ids, _ := s.QARunIDs(); len(ids) != 0 {
		t.Errorf("runs after card delete = %v", ids)
	}
}
