package api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/qa"
	"github.com/arcane-sn/one-piece/backend/internal/store"
)

type qaFix struct {
	s     *testSrv
	card  store.Card
	board store.Board
	item  store.Item
	ip    string
	cp    string
	p     qa.Paths
}

// qaFixture: a board with a QA stack, an approved ticket, one QA Test Case, a ready toolchain and a valid login.
func qaFixture(t *testing.T) *qaFix {
	t.Helper()
	s := newTestServer(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	os.MkdirAll(filepath.Join(root, "web", ".git"), 0o755) // looks like a repo to the repo finder; snapshots are stubbed
	b, _ := s.St.CreateBoard("Midas", "", []string{root})
	if _, err := s.St.SetQAConfig(b.ID, qa.Config{BaseURL: "http://localhost:5000", AllowedHosts: []string{"auth.example.test"},
		Services: []qa.Service{{Name: "web", Folder: filepath.Join(root, "web"), Command: "x", Port: 5000, ReadyURL: "http://localhost:5000/"}}}); err != nil {
		t.Fatal(err)
	}
	c, _ := s.St.CreateCard(b.ID, "Login", "Sign in", "")
	s.St.DB.Exec(`UPDATE cards SET approved=1 WHERE id=?`, c.ID)
	it, _ := s.St.CreateItem(c.ID, "qa", "Login works", "1. open\n2. type\nExpect: dashboard")
	p := qa.Paths{Data: s.St.Dir}
	os.MkdirAll(filepath.Join(p.Runner(), "node_modules", ".bin"), 0o755)
	os.WriteFile(p.PlaywrightBin(), []byte("x"), 0o755)
	os.MkdirAll(p.AuthDir(), 0o700)
	os.WriteFile(p.Auth(b.ID), []byte(`{"cookies":[{"name":"s","value":"abcdefgh12345","expires":`+strconv.FormatInt(time.Now().Add(48*time.Hour).Unix(), 10)+`}]}`), 0o600)

	oldStart, oldLive, oldSnap := qaStartStack, qaLiveCheck, qaSnapshot
	qaStartStack = func(context.Context, []qa.Service, string, string) (*qa.Stack, error) { return &qa.Stack{}, nil }
	qaLiveCheck = func(context.Context, qa.Paths, int64, qa.Config) (bool, string) { return true, "" }
	qaSnapshot = func(r string) (qa.RepoState, error) {
		return qa.RepoState{Repo: r, Branch: "main", SHA: "abcdef123456"}, nil
	}
	t.Cleanup(func() { qaStartStack, qaLiveCheck, qaSnapshot = oldStart, oldLive, oldSnap })
	return &qaFix{s, c, b, it, "/api/items/" + strconv.FormatInt(it.ID, 10), "/api/cards/" + strconv.FormatInt(c.ID, 10), p}
}

// session describes what the stubbed Claude does inside the run.
type session struct {
	class    string // "" = no attempt; passed | assertion | spec | infra
	reason   string
	verdict  string // the final JSON ("" = none)
	read     bool   // opens the screenshots
	blockTxt string // runs qa-blocked
	hosts    []string
}

func (f *qaFix) stub(t *testing.T, sc session) *qaSessionIn {
	t.Helper()
	got := &qaSessionIn{}
	old := agentQA
	t.Cleanup(func() { agentQA = old })
	agentQA = func(_ context.Context, in qaSessionIn, onLog func(string)) (string, error) {
		*got = in
		runDir := f.p.Run(in.RunID)
		if sc.class != "" {
			ad := f.p.Attempt(in.RunID, 1)
			os.MkdirAll(filepath.Join(ad, "steps"), 0o755)
			os.WriteFile(filepath.Join(ad, "steps", "01-open.png"), []byte("png"), 0o644)
			cl, _ := json.Marshal(qa.Class{Kind: sc.class, Reason: sc.reason})
			os.WriteFile(filepath.Join(ad, "class.json"), cl, 0o644)
			h, _ := json.Marshal(sc.hosts)
			os.WriteFile(filepath.Join(ad, qa.BlockedFile), h, 0o644)
			f.s.St.AddQASteps(in.RunID, 1, []qa.Step{{Position: 1, Title: "Open", Status: "passed", Screenshot: "steps/01-open.png"}})
			if sc.read {
				onLog("Reading 01-open.png")
			}
		}
		if sc.blockTxt != "" {
			os.WriteFile(filepath.Join(runDir, "blocked.txt"), []byte(sc.blockTxt), 0o644)
		}
		return sc.verdict, nil
	}
	return got
}

func (f *qaFix) run(t *testing.T) (store.Item, store.QARun) {
	t.Helper()
	resp, body := f.s.req(t, "POST", f.ip+"/run", map[string]any{}, f.s.login(t))
	if resp.StatusCode != 202 {
		t.Fatalf("run: %d %s", resp.StatusCode, body)
	}
	var it store.Item
	for i := 0; i < 300; i++ {
		it, _ = f.s.St.GetItem(f.item.ID)
		if it.Status != "doing" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	runs, _ := f.s.St.ListQARuns(f.item.ID)
	if len(runs) == 0 {
		t.Fatalf("no run row; log = %v", it.Log)
	}
	return it, runs[0]
}

func (f *qaFix) fixTasks() int {
	items, _ := f.s.St.ListItems(f.card.ID)
	n := 0
	for _, x := range items {
		if x.Kind == "agent" && strings.Contains(x.Title, "[FIX]") {
			n++
		}
	}
	return n
}

const passJSON = `{"result":"pass","issue":"","desc":"dashboard shown"}`
const failJSON = `{"result":"fail","issue":"dashboard missing","desc":"the page shows an error"}`

func TestQAOutcomeTable(t *testing.T) {
	for name, c := range map[string]struct {
		sc      session
		status  string // run status
		item    string
		fix     int
		contain string
	}{
		"pass":                  {session{class: "passed", verdict: passJSON, read: true}, "passed", "done", 0, ""},
		"finding":               {session{class: "assertion", verdict: failJSON, read: true}, "failed", "todo", 1, ""},
		"run passed, shots bad": {session{class: "passed", verdict: failJSON, read: true}, "failed", "todo", 1, ""},
		"run failed, said pass": {session{class: "assertion", verdict: passJSON, read: true}, "blocked", "todo", 0, "disagrees"},
		"environment":           {session{class: "infra", reason: "The page could not be reached", verdict: failJSON, read: true}, "blocked", "todo", 0, "could not be reached"},
		"broken spec":           {session{class: "spec", reason: "Broken spec: x", verdict: failJSON, read: true}, "blocked", "todo", 0, "Broken spec"},
		"claude blocked":        {session{class: "assertion", blockTxt: "login page shown", verdict: failJSON, read: true}, "blocked", "todo", 0, "login page shown"},
		"never ran":             {session{verdict: failJSON}, "blocked", "todo", 0, "never got through"},
		"did not look":          {session{class: "passed", verdict: passJSON, read: false}, "blocked", "todo", 0, "did not look"},
		"unreadable":            {session{class: "passed", verdict: "no json here", read: true}, "blocked", "todo", 0, "could not be read"},
	} {
		t.Run(name, func(t *testing.T) {
			f := qaFixture(t)
			f.stub(t, c.sc)
			it, run := f.run(t)
			if run.Status != c.status || it.Status != c.item || f.fixTasks() != c.fix {
				t.Fatalf("run %q item %q fix %d; log %v", run.Status, it.Status, f.fixTasks(), it.Log)
			}
			if c.contain != "" && !strings.Contains(run.BlockReason, c.contain) {
				t.Errorf("block reason = %q", run.BlockReason)
			}
			if run.Status == "blocked" && f.fixTasks() != 0 {
				t.Error("a blocked run must never create a [FIX] task")
			}
		})
	}
}

func TestQAPreflightBlocks(t *testing.T) {
	for name, c := range map[string]struct {
		setup   func(*qaFix)
		contain string
	}{
		"no toolchain": {func(f *qaFix) { os.Remove(f.p.PlaywrightBin()) }, "qa-setup"},
		"no login":     {func(f *qaFix) { os.Remove(f.p.Auth(f.board.ID)) }, "Login needed"},
		"expired login": {func(f *qaFix) {
			os.WriteFile(f.p.Auth(f.board.ID), []byte(`{"cookies":[{"name":"s","value":"x","expires":1000}]}`), 0o600)
		}, "Login needed"},
		"port taken": {func(*qaFix) {
			qaStartStack = func(context.Context, []qa.Service, string, string) (*qa.Stack, error) {
				return nil, errors.New("Port 5000 is already in use by a process One Piece did not start. Stop it and run again.")
			}
		}, "Port 5000"},
		"not logged in live": {func(*qaFix) {
			qaLiveCheck = func(context.Context, qa.Paths, int64, qa.Config) (bool, string) { return false, "Login needed" }
		}, "Login needed"},
	} {
		t.Run(name, func(t *testing.T) {
			f := qaFixture(t)
			called := false
			old := agentQA
			agentQA = func(context.Context, qaSessionIn, func(string)) (string, error) { called = true; return "", nil }
			t.Cleanup(func() { agentQA = old })
			c.setup(f)
			it, run := f.run(t)
			if run.Status != "blocked" || !strings.Contains(run.BlockReason, c.contain) || it.Status != "todo" || f.fixTasks() != 0 || called {
				t.Fatalf("run %+v item %q called=%v", run, it.Status, called)
			}
		})
	}
}

func TestQASessionIsConfinedAndCarriesContext(t *testing.T) {
	f := qaFixture(t)
	f.s.St.AddDecision(f.card.ID, "Login uses the SSO page", "claude")
	got := f.stub(t, session{class: "passed", verdict: passJSON, read: true})
	_, run := f.run(t)
	if run.Status != "passed" {
		t.Fatalf("run = %+v", run)
	}
	spec := f.p.SpecPath(f.board.ID, f.item.Ident)
	if got.Spec != spec || !strings.HasPrefix(spec, f.p.Workspace(f.board.ID)) {
		t.Errorf("spec = %q", got.Spec)
	}
	for _, want := range []string{"Decision log", "SSO page", "http://localhost:5000", spec, "qa-run", "main at abcdef12"} {
		if !strings.Contains(got.Prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, got.Prompt)
		}
	}
	if len(got.DenyEdit) == 0 || got.DenyEdit[0] != f.board.Folders[0] {
		t.Errorf("product folders must be edit-denied: %v", got.DenyEdit)
	}
	joined := strings.Join(got.DenyRead, " ")
	if !strings.Contains(joined, f.p.AuthDir()) || !strings.Contains(joined, f.p.Runner()) {
		t.Errorf("deny read = %v", got.DenyRead)
	}
	for _, d := range got.ReadDirs {
		if strings.HasPrefix(f.p.Auth(f.board.ID), d+string(filepath.Separator)) {
			t.Errorf("the login lives under a readable dir %s", d)
		}
	}
	if fi, err := os.Lstat(spec); err != nil || !fi.Mode().IsRegular() {
		t.Error("the spec file must exist as a regular file before the session")
	}
	if b, _ := os.ReadFile(filepath.Join(f.p.Run(run.ID), "repos.json")); !strings.Contains(string(b), "abcdef123456") {
		t.Errorf("repos.json = %s", b)
	}
	if len(run.Repos) != 1 || run.Repos[0].Branch != "main" {
		t.Errorf("repos on the run = %+v", run.Repos)
	}
}

func TestQARepoBackstop(t *testing.T) {
	f := qaFixture(t)
	f.stub(t, session{class: "passed", verdict: passJSON, read: true})
	var n int32
	qaSnapshot = func(r string) (qa.RepoState, error) {
		st := qa.RepoState{Repo: r, Branch: "main", SHA: "abc"}
		if atomic.AddInt32(&n, 1) > 1 { // the second look (after the session) differs
			st.Status = " M app.js"
			st.Dirty = true
		}
		return st, nil
	}
	it, run := f.run(t)
	if run.Status != "blocked" || !strings.Contains(run.BlockReason, "QA changed") || it.Status != "todo" || f.fixTasks() != 0 {
		t.Fatalf("run = %+v item %q", run, it.Status)
	}
}

func TestQAOneRunAtATime(t *testing.T) {
	f := qaFixture(t)
	second, _ := f.s.St.CreateItem(f.card.ID, "qa", "Second", "x")
	hold, in := make(chan struct{}), make(chan struct{})
	old := agentQA
	t.Cleanup(func() { agentQA = old })
	agentQA = func(context.Context, qaSessionIn, func(string)) (string, error) {
		close(in)
		<-hold
		return "", errors.New("stop")
	}
	ck := f.s.login(t)
	if resp, body := f.s.req(t, "POST", f.ip+"/run", map[string]any{}, ck); resp.StatusCode != 202 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	<-in
	ip2 := "/api/items/" + strconv.FormatInt(second.ID, 10)
	if resp, _ := f.s.req(t, "POST", ip2+"/run", map[string]any{}, ck); resp.StatusCode != 202 {
		t.Fatal("start second")
	}
	for i := 0; i < 200; i++ {
		if it, _ := f.s.St.GetItem(second.ID); it.Status == "todo" && len(it.Log) > 0 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	it2, _ := f.s.St.GetItem(second.ID)
	if !strings.Contains(strings.Join(it2.Log, "\n"), "Another QA run is in progress") {
		t.Errorf("second log = %v", it2.Log)
	}
	// the login button is refused while a run holds the lock
	if resp, _ := f.s.req(t, "POST", "/api/boards/"+strconv.FormatInt(f.board.ID, 10)+"/qa/login", map[string]any{}, ck); resp.StatusCode != 409 {
		t.Errorf("login during a run: %d", resp.StatusCode)
	}
	close(hold)
}

func TestQAUnconfiguredBoardKeepsCodeQA(t *testing.T) {
	f := qaFixture(t)
	f.s.St.SetQAConfig(f.board.ID, qa.Config{})
	called := false
	old, oldQA := streamClaude, agentQA
	t.Cleanup(func() { streamClaude, agentQA = old, oldQA })
	streamClaude = func(_ context.Context, system, _ string, _ []string, _ func(string)) (string, error) {
		called = strings.Contains(system, "only read code")
		return passJSON, nil
	}
	agentQA = func(context.Context, qaSessionIn, func(string)) (string, error) {
		t.Error("browser QA must not run")
		return "", nil
	}
	it, _ := f.s.St.GetItem(f.item.ID)
	f.s.St.DB.Exec(`UPDATE boards SET id=id WHERE id=?`, f.board.ID)
	if folders := f.board.Folders; len(folders) == 0 {
		t.Fatal("no folder")
	}
	resp, body := f.s.req(t, "POST", f.ip+"/run", map[string]any{}, f.s.login(t))
	if resp.StatusCode != 202 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	for i := 0; i < 300; i++ {
		if it, _ = f.s.St.GetItem(f.item.ID); it.Status != "doing" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !called || it.Status != "done" {
		t.Errorf("code QA: called=%v status=%q log=%v", called, it.Status, it.Log)
	}
}
