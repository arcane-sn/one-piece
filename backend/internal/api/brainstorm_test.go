package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

type bsView struct {
	Messages []store.BrainstormMessage `json:"messages"`
	Running  bool                      `json:"running"`
	Plan     *store.PlanVersion        `json:"plan"`
	Versions []store.PlanVersion       `json:"versions"`
}

type bsFn = func(ctx context.Context, in brainstormRun, onText func(string)) (string, error)

// useBrainstorm swaps the Claude runner for the test and records every call it receives.
func useBrainstorm(t *testing.T, fn bsFn) *[]brainstormRun {
	t.Helper()
	var mu sync.Mutex
	var calls []brainstormRun
	old := runBrainstorm
	runBrainstorm = func(ctx context.Context, in brainstormRun, onText func(string)) (string, error) {
		mu.Lock()
		calls = append(calls, in)
		mu.Unlock()
		return fn(ctx, in, onText)
	}
	t.Cleanup(func() { runBrainstorm = old })
	return &calls
}

func bsGet(t *testing.T, s *testSrv, bp string, ck *http.Cookie) bsView {
	t.Helper()
	resp, body := s.req(t, "GET", bp+"/brainstorm", nil, ck)
	if resp.StatusCode != 200 {
		t.Fatalf("get brainstorm: %d %s", resp.StatusCode, body)
	}
	var v bsView
	json.Unmarshal(body, &v)
	return v
}

func bsIdle(t *testing.T, s *testSrv, bp string, ck *http.Cookie) bsView {
	t.Helper()
	for i := 0; i < 200; i++ {
		if v := bsGet(t, s, bp, ck); !v.Running {
			return v
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("the turn never finished")
	return bsView{}
}

func bsSend(t *testing.T, s *testSrv, bp string, ck *http.Cookie, text string) int {
	t.Helper()
	resp, _ := s.req(t, "POST", bp+"/brainstorm/messages", map[string]string{"text": text}, ck)
	return resp.StatusCode
}

func bsBoardID(bp string) int64 {
	id, _ := strconv.ParseInt(bp[strings.LastIndex(bp, "/")+1:], 10, 64)
	return id
}

func bsSetup(t *testing.T) (*testSrv, string, *http.Cookie, int64) {
	t.Helper()
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	return s, bp, ck, bsBoardID(bp)
}

func TestBrainstormSendStoresMessagesAndSession(t *testing.T) {
	calls := useBrainstorm(t, func(_ context.Context, _ brainstormRun, onText func(string)) (string, error) {
		onText("hi")
		return "sess-1", nil
	})
	s, bp, ck, bid := bsSetup(t)
	if code := bsSend(t, s, bp, ck, "Plan the login"); code != 202 {
		t.Fatalf("send = %d", code)
	}
	v := bsIdle(t, s, bp, ck)
	if len(v.Messages) != 2 || v.Messages[0].Role != "user" || v.Messages[0].Text != "Plan the login" ||
		v.Messages[1].Role != "assistant" || v.Messages[1].Text != "hi" || v.Messages[1].Status != "done" {
		t.Fatalf("messages = %+v", v.Messages)
	}
	if st, _ := s.St.GetBrainstormState(bid); st.SessionID != "sess-1" {
		t.Fatalf("state = %+v", st)
	}
	in := (*calls)[0]
	if in.SessionID != "" || in.BoardID != bid || in.PlanPath != s.St.PlanPath(bid) || in.DB != s.St.Path ||
		!strings.Contains(in.Prompt, "Plan the login") || !strings.Contains(in.Prompt, in.PlanPath) || !strings.Contains(in.Prompt, "Board: B") {
		t.Fatalf("first call = %+v", in)
	}
}

func TestBrainstormSecondSendWhileRunningIs409(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	useBrainstorm(t, func(ctx context.Context, _ brainstormRun, _ func(string)) (string, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "s", nil
	})
	s, bp, ck, _ := bsSetup(t)
	if code := bsSend(t, s, bp, ck, "one"); code != 202 {
		t.Fatalf("first send = %d", code)
	}
	<-started
	if code := bsSend(t, s, bp, ck, "two"); code != 409 {
		t.Fatalf("second send while running = %d, want 409", code)
	}
	if !bsGet(t, s, bp, ck).Running {
		t.Fatal("running flag should be set")
	}
	if resp, _ := s.req(t, "POST", bp+"/brainstorm/new", map[string]any{}, ck); resp.StatusCode != 409 {
		t.Fatalf("new conversation while running = %d, want 409", resp.StatusCode)
	}
	close(release)
	bsIdle(t, s, bp, ck)
}

func TestBrainstormStopKeepsPartialReplyAsStopped(t *testing.T) {
	started := make(chan struct{}, 1)
	useBrainstorm(t, func(ctx context.Context, _ brainstormRun, onText func(string)) (string, error) {
		onText("partial")
		started <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	})
	s, bp, ck, bid := bsSetup(t)
	bsSend(t, s, bp, ck, "go")
	<-started
	if resp, _ := s.req(t, "POST", bp+"/brainstorm/stop", map[string]any{}, ck); resp.StatusCode != 204 {
		t.Fatalf("stop = %d", resp.StatusCode)
	}
	v := bsIdle(t, s, bp, ck)
	if a := v.Messages[1]; a.Text != "partial" || a.Status != "stopped" {
		t.Fatalf("assistant = %+v", a)
	}
	if st, _ := s.St.GetBrainstormState(bid); st.SessionID != "" {
		t.Fatalf("a stopped turn must not store a session: %+v", st)
	}
}

func TestBrainstormResumeFailureFallsBackToFreshSession(t *testing.T) {
	calls := useBrainstorm(t, func(_ context.Context, in brainstormRun, onText func(string)) (string, error) {
		if in.SessionID == "dead" {
			return "", errors.New("No conversation found with session ID: dead")
		}
		onText("ok")
		return "sess-new", nil
	})
	s, bp, ck, bid := bsSetup(t)
	s.St.SetBrainstormState(bid, store.BrainstormState{SessionID: "dead", SentIndex: ""})
	bsSend(t, s, bp, ck, "hello again")
	v := bsIdle(t, s, bp, ck)
	if a := v.Messages[1]; a.Text != "ok" || a.Status != "done" {
		t.Fatalf("assistant = %+v", a)
	}
	if len(*calls) != 2 || (*calls)[0].SessionID != "dead" || (*calls)[1].SessionID != "" ||
		!strings.Contains((*calls)[1].Prompt, "Board: B") {
		t.Fatalf("calls = %+v", *calls)
	}
	if st, _ := s.St.GetBrainstormState(bid); st.SessionID != "sess-new" {
		t.Fatalf("state = %+v", st)
	}
}

func TestBrainstormResumeSendsOnlyTicketChanges(t *testing.T) {
	calls := useBrainstorm(t, func(_ context.Context, _ brainstormRun, onText func(string)) (string, error) {
		onText("ok")
		return "sess-1", nil
	})
	s, bp, ck, bid := bsSetup(t)
	bsSend(t, s, bp, ck, "first")
	bsIdle(t, s, bp, ck)
	d, _ := s.St.CreateDraft(bid, "Owner login", "")
	bsSend(t, s, bp, ck, "second")
	bsIdle(t, s, bp, ck)
	p := (*calls)[1].Prompt
	if (*calls)[1].SessionID != "sess-1" || !strings.Contains(p, "+ #"+strconv.FormatInt(d.ID, 10)+" draft Owner login") ||
		strings.Contains(p, "Board: B") || !strings.Contains(p, "second") {
		t.Fatalf("resume prompt = %q (session %q)", p, (*calls)[1].SessionID)
	}
	bsSend(t, s, bp, ck, "third")
	bsIdle(t, s, bp, ck)
	if p := (*calls)[2].Prompt; strings.Contains(p, "Tickets changed") {
		t.Fatalf("nothing changed, so nothing should be resent: %q", p)
	}
}

func TestBrainstormSnapshotsPlanWhenFileChanged(t *testing.T) {
	content := []string{"# plan", "# plan", "# plan v2"}
	n := 0
	useBrainstorm(t, func(_ context.Context, in brainstormRun, _ func(string)) (string, error) {
		os.MkdirAll(filepath.Dir(in.PlanPath), 0o755)
		os.WriteFile(in.PlanPath, []byte(content[n]), 0o644)
		n++
		return "s", nil
	})
	s, bp, ck, _ := bsSetup(t)
	want := []int{1, 1, 2}
	for i, w := range want {
		bsSend(t, s, bp, ck, "turn")
		v := bsIdle(t, s, bp, ck)
		if len(v.Versions) != w {
			t.Fatalf("after turn %d: %d versions, want %d", i+1, len(v.Versions), w)
		}
	}
	if v := bsGet(t, s, bp, ck); v.Plan == nil || v.Plan.Version != 2 || v.Plan.Content != "# plan v2" {
		t.Fatalf("plan = %+v", v.Plan)
	}
}

func TestRestorePlanWritesFileAndAddsVersion(t *testing.T) {
	s, bp, ck, bid := bsSetup(t)
	s.St.AddPlanVersion(bid, "one")
	s.St.AddPlanVersion(bid, "two")
	resp, body := s.req(t, "POST", bp+"/plan/1/restore", map[string]any{}, ck)
	if resp.StatusCode != 200 {
		t.Fatalf("restore = %d %s", resp.StatusCode, body)
	}
	if b, _ := os.ReadFile(s.St.PlanPath(bid)); string(b) != "one" {
		t.Fatalf("plan file = %q", b)
	}
	v := bsGet(t, s, bp, ck)
	if len(v.Versions) != 3 || v.Plan.Version != 3 || v.Plan.Content != "one" {
		t.Fatalf("after restore: %+v / %+v", v.Versions, v.Plan)
	}
	if resp, _ := s.req(t, "POST", bp+"/plan/9/restore", map[string]any{}, ck); resp.StatusCode != 404 {
		t.Fatalf("restoring a missing version = %d, want 404", resp.StatusCode)
	}
	if resp, body := s.req(t, "GET", bp+"/plan/2", nil, ck); resp.StatusCode != 200 || !strings.Contains(string(body), `"content":"two"`) {
		t.Fatalf("get version 2 = %d %s", resp.StatusCode, body)
	}
}

func TestIndexChange(t *testing.T) {
	if got := indexChange("#1 draft A", "#1 draft A"); got != "" {
		t.Fatalf("equal indexes: %q", got)
	}
	got := indexChange("#1 draft A\n#2 draft B", "#1 draft A\n#3 draft C")
	if !strings.Contains(got, "+ #3 draft C") || !strings.Contains(got, "- #2 draft B") || strings.Contains(got, "#1 draft A") {
		t.Fatalf("diff = %q", got)
	}
}

func TestDeleteBoardRemovesPlanFile(t *testing.T) {
	s, bp, ck, bid := bsSetup(t)
	path := s.St.PlanPath(bid)
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("secret plan"), 0o644)
	if resp, _ := s.req(t, "DELETE", bp, nil, ck); resp.StatusCode != 204 {
		t.Fatalf("delete board = %d", resp.StatusCode)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("plan file should be gone, stat err = %v", err)
	}
}

func TestBrainstormEmptyBoardViewAndValidation(t *testing.T) {
	s, bp, ck, _ := bsSetup(t)
	resp, body := s.req(t, "GET", bp+"/brainstorm", nil, ck)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"plan":null`) ||
		!strings.Contains(string(body), `"messages":[]`) || !strings.Contains(string(body), `"versions":[]`) {
		t.Fatalf("empty view = %d %s", resp.StatusCode, body)
	}
	if code := bsSend(t, s, bp, ck, "   "); code != 400 {
		t.Fatalf("blank message = %d, want 400", code)
	}
	if resp, _ := s.req(t, "GET", "/api/boards/999/brainstorm", nil, ck); resp.StatusCode != 404 {
		t.Fatalf("unknown board = %d, want 404", resp.StatusCode)
	}
}

func TestBrainstormNewConversationClearsSession(t *testing.T) {
	s, bp, ck, bid := bsSetup(t)
	s.St.SetBrainstormState(bid, store.BrainstormState{SessionID: "x", SentIndex: "#1 draft A"})
	if resp, _ := s.req(t, "POST", bp+"/brainstorm/new", map[string]any{}, ck); resp.StatusCode != 204 {
		t.Fatalf("new = %d", resp.StatusCode)
	}
	if st, _ := s.St.GetBrainstormState(bid); st != (store.BrainstormState{}) {
		t.Fatalf("state = %+v", st)
	}
}
