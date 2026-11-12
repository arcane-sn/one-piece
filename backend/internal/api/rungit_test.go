package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newGitRepo(t *testing.T, dir string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.name", "t")
	gitIn(t, dir, "config", "user.email", "t@example.com")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
}

type gitRun struct {
	s       *testSrv
	ck      *http.Cookie
	cp      string
	card    store.Card
	master  string
	repo    string // registered, on its feature branch
	feature string
}

// gitRunFixture: an approved ticket whose board folder holds repo "svc" (branched) and repo "other" (unregistered).
func gitRunFixture(t *testing.T) *gitRun {
	t.Helper()
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	master, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(master, "svc")
	newGitRepo(t, repo)
	newGitRepo(t, filepath.Join(master, "other"))
	bp := newBoardPath(t, s, ck)
	s.req(t, "PUT", bp, map[string]any{"name": "B", "folders": []string{master}}, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Sign in"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	reviewWaiter(t, s, cp, ck)("done")
	s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck)
	br, err := s.St.PlanBranch(card.ID, repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "switch", "-q", "-c", br.FeatureBranch, "main")
	st := "branched"
	s.St.UpdateBranch(card.ID, repo, store.BranchPatch{Status: &st})
	return &gitRun{s, ck, cp, card, master, repo, br.FeatureBranch}
}

func (g *gitRun) addTask(t *testing.T, title string) (store.Item, string) {
	t.Helper()
	_, body := g.s.req(t, "POST", g.cp+"/items", map[string]any{"kind": "agent", "title": title}, g.ck)
	var it store.Item
	json.Unmarshal(body, &it)
	return it, "/api/items/" + strconv.FormatInt(it.ID, 10)
}

type agentFn = func(ctx context.Context, cwd string, dirs []string, system, user string, onLog func(string)) (string, error)

func useAgent(t *testing.T, fn agentFn) {
	t.Helper()
	old := agentRun
	agentRun = fn
	t.Cleanup(func() { agentRun = old })
}

// writes a file named after the task in cwd, like an agent editing code
func writeInCwd(user, cwd string) {
	name := "task.txt"
	if i := strings.Index(user, "Your Agent Task: "); i >= 0 {
		name = strings.Fields(user[i+len("Your Agent Task: "):])[0] + ".txt"
	}
	os.WriteFile(filepath.Join(cwd, name), []byte("x"), 0o644)
}

// waitLog polls until the item's log contains want.
func waitLog(t *testing.T, g *gitRun, ip, want string) store.Item {
	t.Helper()
	var it store.Item
	for i := 0; i < 150; i++ {
		_, body := g.s.req(t, "GET", g.cp+"/items", nil, g.ck)
		var all []store.Item
		json.Unmarshal(body, &all)
		for _, x := range all {
			if "/api/items/"+strconv.FormatInt(x.ID, 10) == ip {
				it = x
			}
		}
		if strings.Contains(strings.Join(it.Log, "\n"), want) {
			return it
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("log never contained %q: %+v", want, it)
	return it
}

func TestRunRefusesWrongBranch(t *testing.T) {
	g := gitRunFixture(t)
	called := false
	useAgent(t, func(context.Context, string, []string, string, string, func(string)) (string, error) {
		called = true
		return "ok", nil
	})
	gitIn(t, g.repo, "switch", "-q", "main")
	it, ip := g.addTask(t, "Add endpoint")
	resp, body := g.s.req(t, "POST", ip+"/run", map[string]any{}, g.ck)
	if resp.StatusCode != 409 || !strings.Contains(string(body), "expected "+g.feature) || !strings.Contains(string(body), "svc is on main") {
		t.Fatalf("run on wrong branch: %d %s", resp.StatusCode, body)
	}
	if called {
		t.Error("the agent must not run")
	}
	if got, _ := g.s.St.GetItem(it.ID); got.Status != "todo" {
		t.Errorf("status = %q", got.Status)
	}
	if n := gitIn(t, g.repo, "rev-list", "--count", "HEAD"); n != "1" {
		t.Errorf("commits = %s, want 1", n)
	}
}

func TestRunCommitsPerTask(t *testing.T) {
	g := gitRunFixture(t)
	useAgent(t, func(_ context.Context, cwd string, _ []string, _, user string, _ func(string)) (string, error) {
		writeInCwd(user, cwd)
		return "ok", nil
	})
	it, ip := g.addTask(t, "Add endpoint")
	if resp, body := g.s.req(t, "POST", ip+"/run", map[string]any{}, g.ck); resp.StatusCode != 202 {
		t.Fatalf("run: %d %s", resp.StatusCode, body)
	}
	done := waitItem(t, g.s, g.cp, ip, g.ck, "done")
	if got := gitIn(t, g.repo, "log", "-1", "--format=%s"); got != it.Ident+": Add endpoint" {
		t.Fatalf("commit subject = %q", got)
	}
	if st := gitIn(t, g.repo, "status", "--porcelain"); st != "" {
		t.Errorf("tree not clean after the commit: %q", st)
	}
	if !strings.Contains(strings.Join(done.Log, "\n"), "Committed in svc") {
		t.Errorf("log = %v", done.Log)
	}
	if n := gitIn(t, filepath.Join(g.master, "other"), "rev-list", "--count", "HEAD"); n != "1" {
		t.Errorf("an unregistered repo must stay untouched, commits = %s", n)
	}
}

func TestRunScopeUsesRegisteredRepos(t *testing.T) {
	g := gitRunFixture(t)
	var cwd string
	var dirs []string
	useAgent(t, func(_ context.Context, c string, d []string, _, _ string, _ func(string)) (string, error) {
		cwd, dirs = c, d
		return "ok", nil
	})
	_, ip := g.addTask(t, "Scoped")
	g.s.req(t, "POST", ip+"/run", map[string]any{}, g.ck)
	waitItem(t, g.s, g.cp, ip, g.ck, "done")
	if cwd != g.repo || len(dirs) != 0 {
		t.Fatalf("cwd=%q dirs=%v, want the registered repo only", cwd, dirs)
	}

	// a ticket without branching rows keeps the old scope: the board's first folder
	_, body := g.s.req(t, "POST", "/api/boards/"+strconv.FormatInt(g.card.BoardID, 10)+"/cards", map[string]any{"title": "Plain"}, g.ck)
	var plain store.Card
	json.Unmarshal(body, &plain)
	pcp := "/api/cards/" + strconv.FormatInt(plain.ID, 10)
	reviewWaiter(t, g.s, pcp, g.ck)("done")
	g.s.req(t, "POST", pcp+"/approve", map[string]bool{"approved": true}, g.ck)
	_, body = g.s.req(t, "POST", pcp+"/items", map[string]any{"kind": "agent", "title": "Old way"}, g.ck)
	var pit store.Item
	json.Unmarshal(body, &pit)
	pip := "/api/items/" + strconv.FormatInt(pit.ID, 10)
	cwd, dirs = "", nil
	g.s.req(t, "POST", pip+"/run", map[string]any{}, g.ck)
	waitItem(t, g.s, pcp, pip, g.ck, "done")
	if cwd != g.master {
		t.Fatalf("no rows: cwd=%q, want %q", cwd, g.master)
	}
}

func TestCommitFailureBlocksTask(t *testing.T) {
	g := gitRunFixture(t)
	hook := filepath.Join(g.repo, ".git", "hooks", "pre-commit")
	os.MkdirAll(filepath.Dir(hook), 0o755)
	os.WriteFile(hook, []byte("#!/bin/sh\necho nope >&2\nexit 1\n"), 0o755)
	useAgent(t, func(_ context.Context, cwd string, _ []string, _, user string, _ func(string)) (string, error) {
		writeInCwd(user, cwd)
		return "ok", nil
	})
	_, ip1 := g.addTask(t, "first")
	_, ip2 := g.addTask(t, "second")
	if resp, body := g.s.req(t, "POST", g.cp+"/run-all", map[string]any{}, g.ck); resp.StatusCode != 202 {
		t.Fatalf("run-all: %d %s", resp.StatusCode, body)
	}
	one := waitLog(t, g, ip1, "could not commit")
	joined := strings.Join(one.Log, "\n")
	if !strings.Contains(joined, "Failed: could not commit in svc") || !strings.Contains(joined, "nope") {
		t.Fatalf("log = %v", one.Log)
	}
	time.Sleep(150 * time.Millisecond)
	_, body := g.s.req(t, "GET", g.cp+"/items", nil, g.ck)
	var all []store.Item
	json.Unmarshal(body, &all)
	for _, x := range all {
		if "/api/items/"+strconv.FormatInt(x.ID, 10) == ip2 && (x.Status != "todo" || len(x.Log) != 1) {
			t.Errorf("the chain must stop: second task = %+v", x)
		}
	}
}

func TestRunAllChecksBranchOnce(t *testing.T) {
	g := gitRunFixture(t)
	useAgent(t, func(_ context.Context, cwd string, _ []string, _, user string, _ func(string)) (string, error) {
		writeInCwd(user, cwd)
		return "ok", nil
	})
	_, ip1 := g.addTask(t, "one")
	_, ip2 := g.addTask(t, "two")
	// after the first commit the repo falls back to main, as if someone switched it
	hook := filepath.Join(g.repo, ".git", "hooks", "post-commit")
	os.MkdirAll(filepath.Dir(hook), 0o755)
	os.WriteFile(hook, []byte("#!/bin/sh\ngit switch -q main\n"), 0o755)
	if resp, body := g.s.req(t, "POST", g.cp+"/run-all", map[string]any{}, g.ck); resp.StatusCode != 202 {
		t.Fatalf("run-all: %d %s", resp.StatusCode, body)
	}
	waitItem(t, g.s, g.cp, ip1, g.ck, "done")
	var two store.Item
	for i := 0; i < 150; i++ {
		_, body := g.s.req(t, "GET", g.cp+"/items", nil, g.ck)
		var all []store.Item
		json.Unmarshal(body, &all)
		for _, x := range all {
			if "/api/items/"+strconv.FormatInt(x.ID, 10) == ip2 {
				two = x
			}
		}
		if strings.Contains(strings.Join(two.Log, "\n"), "Blocked:") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(strings.Join(two.Log, "\n"), "Blocked: ") || two.Status != "todo" {
		t.Fatalf("second task = %+v", two)
	}
	if n := gitIn(t, g.repo, "rev-list", "--count", "main.."+g.feature); n != "1" {
		t.Errorf("commits on the feature branch = %s, want 1", n)
	}
}

func TestCommitRefusedWhenRepoLeftTheBranch(t *testing.T) {
	g := gitRunFixture(t)
	useAgent(t, func(_ context.Context, cwd string, _ []string, _, user string, _ func(string)) (string, error) {
		writeInCwd(user, cwd)
		gitIn(t, cwd, "stash", "-u", "-q") // clean tree, then move off the branch with the file stashed away
		gitIn(t, cwd, "switch", "-q", "main")
		gitIn(t, cwd, "stash", "pop", "-q")
		return "ok", nil
	})
	it, ip := g.addTask(t, "wanders")
	g.s.req(t, "POST", ip+"/run", map[string]any{}, g.ck)
	got := waitLog(t, g, ip, "could not commit")
	_ = it
	if joined := strings.Join(got.Log, "\n"); !strings.Contains(joined, "Failed: could not commit in svc") {
		t.Fatalf("log = %v", got.Log)
	}
	if n := gitIn(t, g.repo, "rev-list", "--count", "main"); n != "1" {
		t.Errorf("main must not get the commit, commits = %s", n)
	}
}
