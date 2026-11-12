package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func waitItem(t *testing.T, s *testSrv, cp, ip string, ck *http.Cookie, want string) store.Item {
	t.Helper()
	var it store.Item
	for i := 0; i < 100; i++ {
		_, body := s.req(t, "GET", cp+"/items", nil, ck)
		var all []store.Item
		json.Unmarshal(body, &all)
		for _, x := range all {
			if "/api/items/"+strconv.FormatInt(x.ID, 10) == ip {
				it = x
			}
		}
		if it.Status == want {
			return it
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("item never reached %q, last %+v", want, it)
	return it
}

func TestRunItemExecutesAndStops(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	if resp, body := s.req(t, "PUT", bp, map[string]any{"name": "B", "folders": []string{t.TempDir()}}, ck); resp.StatusCode != 200 {
		t.Fatalf("add folder: %d %s", resp.StatusCode, body)
	}
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Sign in"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	_, body = s.req(t, "POST", cp+"/items", map[string]any{"kind": "agent", "title": "Add endpoint", "body": "POST /api/login"}, ck)
	var it store.Item
	json.Unmarshal(body, &it)
	ip := "/api/items/" + strconv.FormatInt(it.ID, 10)

	if resp, _ := s.req(t, "POST", ip+"/run", map[string]any{}, ck); resp.StatusCode != 409 {
		t.Fatalf("run before approval: %d, want 409", resp.StatusCode)
	}
	s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck)

	var gotCwd, gotUser string
	defer func(f func(context.Context, string, []string, string, string, func(string)) (string, error)) {
		agentRun = f
	}(agentRun)
	agentRun = func(_ context.Context, cwd string, _ []string, _, user string, onLog func(string)) (string, error) {
		gotCwd, gotUser = cwd, user
		onLog("Editing main.go")
		return "Added the endpoint.", nil
	}
	if resp, body := s.req(t, "POST", ip+"/run", map[string]any{}, ck); resp.StatusCode != 202 {
		t.Fatalf("run: %d %s", resp.StatusCode, body)
	}
	done := waitItem(t, s, cp, ip, ck, "done")
	if gotCwd == "" || !strings.Contains(gotUser, "POST /api/login") {
		t.Errorf("cwd=%q prompt=%q", gotCwd, gotUser)
	}
	if last := done.Log[len(done.Log)-1]; !strings.HasPrefix(last, "Done: Added") {
		t.Errorf("log = %v", done.Log)
	}
	if resp, _ := s.req(t, "POST", ip+"/run", map[string]any{}, ck); resp.StatusCode != 409 {
		t.Fatalf("rerun of done: %d, want 409", resp.StatusCode)
	}

	// stop a long run
	s.req(t, "PUT", ip, map[string]any{"title": "Add endpoint", "body": "x", "status": "todo"}, ck)
	agentRun = func(ctx context.Context, _ string, _ []string, _, _ string, _ func(string)) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	s.req(t, "POST", ip+"/run", map[string]any{}, ck)
	waitItem(t, s, cp, ip, ck, "doing")
	if resp, _ := s.req(t, "POST", ip+"/run", map[string]any{}, ck); resp.StatusCode != 409 {
		t.Fatalf("double run: %d, want 409", resp.StatusCode)
	}
	s.req(t, "POST", ip+"/stop", map[string]any{}, ck)
	stopped := waitItem(t, s, cp, ip, ck, "todo")
	if stopped.Log[len(stopped.Log)-1] != "Stopped" {
		t.Errorf("log = %v", stopped.Log)
	}
}

func TestRunAllStopsAtFirstFailure(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	s.req(t, "PUT", bp, map[string]any{"name": "B", "folders": []string{t.TempDir()}}, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Sign in"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	reviewWaiter(t, s, cp, ck)("done")
	s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck)
	var paths []string
	for _, title := range []string{"one", "two", "three"} {
		_, body = s.req(t, "POST", cp+"/items", map[string]any{"kind": "agent", "title": title}, ck)
		var it store.Item
		json.Unmarshal(body, &it)
		paths = append(paths, "/api/items/"+strconv.FormatInt(it.ID, 10))
	}
	defer func(f func(context.Context, string, []string, string, string, func(string)) (string, error)) {
		agentRun = f
	}(agentRun)
	agentRun = func(_ context.Context, _ string, _ []string, _, user string, _ func(string)) (string, error) {
		if strings.Contains(user, "-T2 two") {
			return "", errors.New("build is broken")
		}
		return "ok", nil
	}
	if resp, body := s.req(t, "POST", cp+"/run-all", map[string]any{}, ck); resp.StatusCode != 202 {
		t.Fatalf("run-all: %d %s", resp.StatusCode, body)
	}
	waitItem(t, s, cp, paths[0], ck, "done")
	two := waitItem(t, s, cp, paths[1], ck, "todo")
	if last := two.Log[len(two.Log)-1]; !strings.Contains(last, "Failed: ") || !strings.Contains(last, "build is broken") {
		t.Errorf("blocked log = %v", two.Log)
	}
	time.Sleep(100 * time.Millisecond)
	if three := waitItem(t, s, cp, paths[2], ck, "todo"); strings.Contains(strings.Join(three.Log, "|"), "Run started") {
		t.Errorf("third task must not start: %v", three.Log)
	}
}

func TestRunAllQAWaitsForAgentsAndKeepsGoingAfterFail(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	s.req(t, "PUT", bp, map[string]any{"name": "B", "folders": []string{t.TempDir()}}, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Sign in"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	reviewWaiter(t, s, cp, ck)("done")
	s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck)
	mk := func(kind, title string) string {
		_, body := s.req(t, "POST", cp+"/items", map[string]any{"kind": kind, "title": title}, ck)
		var it store.Item
		json.Unmarshal(body, &it)
		return "/api/items/" + strconv.FormatInt(it.ID, 10)
	}
	agent := mk("agent", "build")
	q1, q2, q3 := mk("qa", "good"), mk("qa", "bad"), mk("qa", "also good")

	defer func(f func(context.Context, string, string, []string, func(string)) (string, error)) {
		streamClaude = f
	}(streamClaude)
	streamClaude = func(_ context.Context, _, user string, _ []string, _ func(string)) (string, error) {
		if strings.Contains(user, "-QA2 bad") {
			return `{"result":"fail","issue":"nav still visible","desc":"close() never unhides the nav in index.html"}`, nil
		}
		return "```json\n" + `{"result":"pass","issue":"","desc":"ok"}` + "\n```", nil
	}
	if resp, _ := s.req(t, "POST", cp+"/run-all", map[string]any{"kind": "qa"}, ck); resp.StatusCode != 409 {
		t.Fatalf("QA before agents are done: %d, want 409", resp.StatusCode)
	}
	s.req(t, "PUT", agent, map[string]any{"title": "build", "status": "done"}, ck)
	if resp, body := s.req(t, "POST", cp+"/run-all", map[string]any{"kind": "qa"}, ck); resp.StatusCode != 202 {
		t.Fatalf("run-all qa: %d %s", resp.StatusCode, body)
	}
	waitItem(t, s, cp, q1, ck, "done")
	waitItem(t, s, cp, q3, ck, "done")
	bad := waitItem(t, s, cp, q2, ck, "todo")
	if bad.Failure != "nav still visible" || bad.FailureDesc != "close() never unhides the nav in index.html" {
		t.Errorf("failure = %q / %q", bad.Failure, bad.FailureDesc)
	}
	if !strings.HasPrefix(bad.Log[len(bad.Log)-2], "Failed: nav still visible") || !strings.HasPrefix(bad.Log[len(bad.Log)-1], "Created Agent Task: [FIX] bad") {
		t.Errorf("log = %v", bad.Log)
	}
	fixes := func() (out []store.Item) {
		_, body := s.req(t, "GET", cp+"/items", nil, ck)
		var all []store.Item
		json.Unmarshal(body, &all)
		for _, x := range all {
			if x.FixFor == bad.ID {
				out = append(out, x)
			}
		}
		return
	}
	f := fixes()
	if len(f) != 1 || f[0].Kind != "agent" || f[0].Title != "[FIX] bad" || !strings.Contains(f[0].Body, "Issue: nav still visible\n\nDesc: close() never") || f[0].Status != "todo" {
		t.Fatalf("fix tasks = %+v", f)
	}
	// QA waits until the FIX task is done
	if resp, _ := s.req(t, "POST", q2+"/run", map[string]any{}, ck); resp.StatusCode != 409 {
		t.Fatalf("run QA with open FIX: %d, want 409", resp.StatusCode)
	}
	fixPath := "/api/items/" + strconv.FormatInt(f[0].ID, 10)
	s.req(t, "PUT", fixPath, map[string]any{"title": f[0].Title, "body": f[0].Body, "status": "done"}, ck)
	// still failing after the fix: a new FIX task says the earlier one did not work
	s.req(t, "POST", q2+"/run", map[string]any{}, ck)
	waitItem(t, s, cp, q2, ck, "todo")
	time.Sleep(100 * time.Millisecond)
	if f = fixes(); len(f) != 2 || !strings.Contains(f[1].Body, "belum berhasil") {
		t.Fatalf("second fix = %+v", f)
	}
	// passing clears the failure
	streamClaude = func(context.Context, string, string, []string, func(string)) (string, error) {
		return `{"result":"pass","issue":"","desc":"fixed"}`, nil
	}
	s.req(t, "PUT", "/api/items/"+strconv.FormatInt(f[1].ID, 10), map[string]any{"title": f[1].Title, "status": "done"}, ck)
	s.req(t, "POST", q2+"/run", map[string]any{}, ck)
	if ok := waitItem(t, s, cp, q2, ck, "done"); ok.Failure != "" || ok.FailureDesc != "" {
		t.Errorf("failure not cleared: %q", ok.Failure)
	}
}
