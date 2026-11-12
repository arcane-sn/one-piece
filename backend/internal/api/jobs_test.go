package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// gen POSTs a Generate endpoint and, when it answers 202, waits for its job to leave "running".
func (s *testSrv) gen(t *testing.T, path string, body map[string]any, ck *http.Cookie) (int, store.Job) {
	t.Helper()
	resp, out := s.req(t, "POST", path, body, ck)
	if resp.StatusCode != http.StatusAccepted {
		return resp.StatusCode, store.Job{}
	}
	var j store.Job
	if err := json.Unmarshal(out, &j); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		got, err := s.St.GetJob(j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "running" {
			return resp.StatusCode, got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %d never finished", j.ID)
	return 0, j
}

func TestGenerateSurvivesTheRequestAndRejectsDoubleStart(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	reviewWaiter(t, s, cp, ck)("done")
	s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck)

	release := make(chan struct{})
	defer func(f func(context.Context, string, string) (string, error)) { runClaude = f }(runClaude)
	runClaude = func(context.Context, string, string) (string, error) {
		<-release
		return itemsReply, nil
	}
	resp, body := s.req(t, "POST", cp+"/generate-items", map[string]any{}, ck)
	if resp.StatusCode != 202 {
		t.Fatalf("start: %d %s", resp.StatusCode, body)
	}
	// the request is long over; the job is still running and a modal opened now sees it
	_, body = s.req(t, "GET", "/api/jobs?scope=card&target="+strconv.FormatInt(card.ID, 10), nil, ck)
	var js []store.Job
	json.Unmarshal(body, &js)
	if len(js) != 1 || js[0].Status != "running" || js[0].Kind != "agent_tasks" || js[0].Label != "Agent Tasks "+card.Ident {
		t.Fatalf("running job = %s", body)
	}
	if resp, _ = s.req(t, "POST", cp+"/generate-items", map[string]any{}, ck); resp.StatusCode != 409 {
		t.Fatalf("second start while running: %d, want 409", resp.StatusCode)
	}
	if _, body = s.req(t, "GET", "/api/jobs/active", nil, ck); len(body) < 10 {
		t.Fatalf("active = %s", body)
	}
	close(release)
	for i := 0; i < 500; i++ {
		if j, _ := s.St.GetJob(js[0].ID); j.Status == "done" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	j, _ := s.St.GetJob(js[0].ID)
	if j.Status != "done" || j.Seen {
		t.Fatalf("finished = %+v", j)
	}
	if resp, _ = s.req(t, "POST", "/api/jobs/"+strconv.FormatInt(j.ID, 10)+"/seen", map[string]any{}, ck); resp.StatusCode != 204 {
		t.Fatalf("seen: %d", resp.StatusCode)
	}
	if act, _ := s.St.ActiveJobs(); len(act) != 0 {
		t.Fatalf("seen job still active: %+v", act)
	}
}

func TestRegenerateStrikesToDoAndSkipsThemInRuns(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	reviewWaiter(t, s, cp, ck)("done")
	s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck)
	old, _ := s.St.CreateItem(card.ID, "agent", "old todo", "")
	kept, _ := s.St.CreateItem(card.ID, "agent", "already done", "")
	s.St.SetItemStatus(kept.ID, "done")

	defer func(f func(context.Context, string, string) (string, error)) { runClaude = f }(runClaude)
	runClaude = func(context.Context, string, string) (string, error) { return itemsReply, nil }
	if code, job := s.gen(t, cp+"/generate-items", map[string]any{}, ck); code != 202 || job.Status != "done" {
		t.Fatalf("regenerate: %d %+v", code, job)
	}
	if got, _ := s.St.GetItem(old.ID); !got.Superseded {
		t.Fatal("the old To Do task should be struck through")
	}
	if got, _ := s.St.GetItem(kept.ID); got.Superseded {
		t.Fatal("a Done task must stay active")
	}
	if resp, _ := s.req(t, "POST", "/api/items/"+strconv.FormatInt(old.ID, 10)+"/run", map[string]any{}, ck); resp.StatusCode != 409 {
		t.Fatalf("running a struck task: %d, want 409", resp.StatusCode)
	}
	if resp, _ := s.req(t, "POST", "/api/items/"+strconv.FormatInt(old.ID, 10)+"/restore", map[string]any{}, ck); resp.StatusCode != 200 {
		t.Fatalf("restore: %d", resp.StatusCode)
	}
	if got, _ := s.St.GetItem(old.ID); got.Superseded {
		t.Fatal("restore should bring it back")
	}
}
