package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

type dView struct {
	Messages []store.DiscussMessage `json:"messages"`
	Running  bool                   `json:"running"`
	Stage    string                 `json:"stage"`
}

type dFn = func(ctx context.Context, in discussRun, onText func(string)) (string, error)

func useDiscuss(t *testing.T, fn dFn) *[]discussRun {
	t.Helper()
	var mu sync.Mutex
	var calls []discussRun
	old := runDiscuss
	runDiscuss = func(ctx context.Context, in discussRun, onText func(string)) (string, error) {
		mu.Lock()
		calls = append(calls, in)
		mu.Unlock()
		return fn(ctx, in, onText)
	}
	t.Cleanup(func() { runDiscuss = old })
	return &calls
}

func dGet(t *testing.T, s *testSrv, cp string, ck *http.Cookie) dView {
	t.Helper()
	resp, body := s.req(t, "GET", cp+"/discuss", nil, ck)
	if resp.StatusCode != 200 {
		t.Fatalf("get discuss: %d %s", resp.StatusCode, body)
	}
	var v dView
	json.Unmarshal(body, &v)
	return v
}

func dIdle(t *testing.T, s *testSrv, cp string, ck *http.Cookie) dView {
	t.Helper()
	for i := 0; i < 200; i++ {
		if v := dGet(t, s, cp, ck); !v.Running {
			return v
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("the discuss turn never finished")
	return dView{}
}

func dSend(t *testing.T, s *testSrv, cp string, ck *http.Cookie, text string) (int, string) {
	t.Helper()
	resp, body := s.req(t, "POST", cp+"/discuss/messages", map[string]string{"text": text}, ck)
	return resp.StatusCode, string(body)
}

// dFixture: a board with a folder, one business card.
func dFixture(t *testing.T) (s *testSrv, ck *http.Cookie, cp string, card store.Card, folder string) {
	t.Helper()
	s = newTestServer(t)
	ck = s.login(t)
	folder, _ = filepath.EvalSymlinks(t.TempDir())
	bp := newBoardPath(t, s, ck)
	s.req(t, "PUT", bp, map[string]any{"name": "Midas", "folders": []string{folder}}, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Sign in"}, ck)
	json.Unmarshal(body, &card)
	return s, ck, "/api/cards/" + strconv.FormatInt(card.ID, 10), card, folder
}

func TestDiscussOnlyInBusinessAndDone(t *testing.T) {
	calls := useDiscuss(t, func(_ context.Context, _ discussRun, onText func(string)) (string, error) {
		onText("which repos?")
		return "sess-1", nil
	})
	s, ck, cp, card, _ := dFixture(t)
	if code, body := dSend(t, s, cp, ck, "let's go"); code != 202 {
		t.Fatalf("business: %d %s", code, body)
	}
	v := dIdle(t, s, cp, ck)
	if v.Stage != "setup" || len(v.Messages) != 2 || v.Messages[1].Text != "which repos?" || v.Messages[1].Status != "done" {
		t.Fatalf("view = %+v", v)
	}
	if sid, _ := s.St.GetDiscussState(card.ID); sid != "sess-1" {
		t.Fatalf("session = %q", sid)
	}
	in := (*calls)[0]
	if in.CardID != card.ID || in.DB != s.St.Path || in.SessionID != "" || !strings.Contains(in.Prompt, card.Ident) || !strings.Contains(in.Prompt, "let's go") {
		t.Fatalf("call = %+v", in)
	}
	if !strings.Contains(in.System, "setup") {
		t.Errorf("system prompt is not the setup one")
	}
	// resumed turn passes the session id
	dSend(t, s, cp, ck, "again")
	dIdle(t, s, cp, ck)
	if got := (*calls)[1].SessionID; got != "sess-1" {
		t.Errorf("resume id = %q", got)
	}

	s.req(t, "POST", cp+"/move", map[string]any{"status": "agent", "position": 0}, ck)
	if code, body := dSend(t, s, cp, ck, "x"); code != 409 || !strings.Contains(body, "Business or Done") {
		t.Fatalf("agent column: %d %s", code, body)
	}
	if v := dGet(t, s, cp, ck); v.Stage != "" {
		t.Errorf("stage in agent column = %q", v.Stage)
	}
	s.req(t, "POST", cp+"/move", map[string]any{"status": "done", "position": 0}, ck)
	if code, body := dSend(t, s, cp, ck, "wrap up"); code != 202 {
		t.Fatalf("done: %d %s", code, body)
	}
	if v := dIdle(t, s, cp, ck); v.Stage != "closeout" {
		t.Errorf("stage = %q", v.Stage)
	}
	if got := (*calls)[2].System; !strings.Contains(got, "closeout") {
		t.Errorf("closeout system prompt missing")
	}
}

func TestDiscussNeedsBoardFolder(t *testing.T) {
	useDiscuss(t, func(context.Context, discussRun, func(string)) (string, error) { return "s", nil })
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	if code, _ := dSend(t, s, "/api/cards/"+strconv.FormatInt(card.ID, 10), ck, "hi"); code != 409 {
		t.Fatalf("no folder: %d, want 409", code)
	}
}

func TestDiscussPromptStages(t *testing.T) {
	master, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(master, "svc")
	os.MkdirAll(repo, 0o755)
	exec.Command("git", "-C", repo, "init", "-q").Run()
	b := store.Board{Name: "Midas", Folders: []string{master}, BranchPrefix: "feature/", DevBranch: "dev"}
	c := store.Card{Ident: "MID-7", Title: "Login", Description: "Sign in", Status: "business"}
	rows := []store.Branch{{Repo: repo, BaseBranch: "main", FeatureBranch: "feature/MID-7", DevBranch: "feature/MID-7-dev", Status: "branched"}}
	const exe = "/tmp/one-piece-agent/op"

	setup := discussPrompt(b, c, nil, exe, "setup", "start", true)
	for _, want := range []string{"MID-7", "feature/", "dev", exe, "none yet", "start"} {
		if !strings.Contains(setup, want) {
			t.Errorf("setup prompt lacks %q:\n%s", want, setup)
		}
	}
	closeout := discussPrompt(b, c, rows, exe, "closeout", "wrap up", true)
	for _, want := range []string{repo, "feature/MID-7-dev", "branched", "closeout"} {
		if !strings.Contains(closeout, want) {
			t.Errorf("closeout prompt lacks %q:\n%s", want, closeout)
		}
	}
	if m := discussPrompt(b, c, rows, exe, "setup", "see @"+filepath.Base(master)+"/svc", true); !strings.Contains(m, repo) {
		t.Errorf("an @-mentioned repo must be named in the prompt")
	}
	resumed := discussPrompt(b, c, rows, exe, "setup", "next", false)
	if strings.Contains(resumed, "Ticket description") || !strings.Contains(resumed, "next") || !strings.Contains(resumed, "branched") {
		t.Errorf("resumed prompt should carry the state, not the whole context:\n%s", resumed)
	}

	raw := regexp.MustCompile(`\bgit (switch|merge|push|fetch|status|branch|rev-parse|log|checkout|reset)\b`)
	for _, stage := range []string{"setup", "closeout"} {
		sys := discussSystemPrompt(stage)
		if raw.MatchString(sys) {
			t.Errorf("%s system prompt has a raw git instruction: %q", stage, raw.FindString(sys))
		}
		if !strings.Contains(sys, "is not permitted") || !strings.Contains(sys, "ask") {
			t.Errorf("%s system prompt lacks the stop-and-ask rules", stage)
		}
	}
	if sys := discussSystemPrompt("closeout"); !strings.Contains(sys, "-dev") || !strings.Contains(sys, "gh <repo> pr create") {
		t.Errorf("closeout prompt must cover the -dev branch and the PR")
	}
	if sys := discussSystemPrompt("setup"); !strings.Contains(sys, "branch-plan") || !strings.Contains(sys, "base branch") {
		t.Errorf("setup prompt must cover branch-plan and asking for the base")
	}
	if raw.MatchString(setup) || raw.MatchString(closeout) {
		t.Errorf("user prompt has a raw git instruction")
	}
	if st, ok := discussStage("business"); !ok || st != "setup" {
		t.Error("business → setup")
	}
	if st, ok := discussStage("done"); !ok || st != "closeout" {
		t.Error("done → closeout")
	}
	for _, other := range []string{"draft", "agent", "qa"} {
		if _, ok := discussStage(other); ok {
			t.Errorf("%s must not allow Discuss", other)
		}
	}
}

func TestDiscussToolsAndEnv(t *testing.T) {
	var got sessionSpec
	old := runSessionFn
	runSessionFn = func(_ context.Context, spec sessionSpec, _ func(string)) (string, error) {
		got = spec
		return "s", nil
	}
	defer func() { runSessionFn = old }()
	in := discussRun{System: "S", Prompt: "P", Exe: "/x/op", DB: "/db", BoardID: 3, CardID: 9, Dirs: []string{"/m"}}
	if _, err := realRunDiscuss(context.Background(), in, func(string) {}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got.AllowedTools, "|")
	for _, want := range []string{"Read", "Grep", "Glob", "Bash(/x/op git:*)", "Bash(/x/op gh:*)", "Bash(/x/op branches:*)", "Bash(/x/op branch-plan:*)", "Bash(/x/op branch-status:*)", "Bash(/x/op branch-pr:*)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("tools lack %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "Write") || strings.Contains(joined, "Edit") {
		t.Errorf("Discuss must not edit files: %s", joined)
	}
	env := strings.Join(got.Env, "|")
	for _, want := range []string{"ONE_PIECE_DB=/db", "ONE_PIECE_BOARD=3", "ONE_PIECE_CARD=9"} {
		if !strings.Contains(env, want) {
			t.Errorf("env lacks %q: %s", want, env)
		}
	}
	if len(got.Dirs) != 1 || got.Dirs[0] != "/m" {
		t.Errorf("dirs = %v", got.Dirs)
	}
}

func TestDiscussStopAndNew(t *testing.T) {
	started := make(chan struct{}, 1)
	useDiscuss(t, func(ctx context.Context, in discussRun, onText func(string)) (string, error) {
		if in.OnSession != nil {
			in.OnSession("sess-live")
		}
		onText("working")
		started <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	})
	s, ck, cp, card, _ := dFixture(t)
	if code, body := dSend(t, s, cp, ck, "go"); code != 202 {
		t.Fatalf("send: %d %s", code, body)
	}
	<-started
	if resp, _ := s.req(t, "POST", cp+"/discuss/new", map[string]any{}, ck); resp.StatusCode != 409 {
		t.Fatalf("new while running: %d, want 409", resp.StatusCode)
	}
	if resp, _ := s.req(t, "POST", cp+"/discuss/stop", map[string]any{}, ck); resp.StatusCode != 204 {
		t.Fatalf("stop: %d", resp.StatusCode)
	}
	v := dIdle(t, s, cp, ck)
	last := v.Messages[len(v.Messages)-1]
	if last.Status != "stopped" || last.Text != "working" {
		t.Fatalf("stopped message = %+v", last)
	}
	if sid, _ := s.St.GetDiscussState(card.ID); sid != "sess-live" {
		t.Fatalf("a stopped first turn must keep its session, got %q", sid)
	}
	if resp, _ := s.req(t, "POST", cp+"/discuss/new", map[string]any{}, ck); resp.StatusCode != 204 {
		t.Fatalf("new: %d", resp.StatusCode)
	}
	if sid, _ := s.St.GetDiscussState(card.ID); sid != "" {
		t.Fatalf("session after new = %q", sid)
	}
	v = dGet(t, s, cp, ck)
	if m := v.Messages[len(v.Messages)-1]; m.Role != "marker" {
		t.Fatalf("new must leave a marker, got %+v", m)
	}
}
