package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/qa"
)

func TestQAConfigRoutes(t *testing.T) {
	f := qaFixture(t)
	ck := f.s.login(t)
	bp := "/api/boards/" + strconv.FormatInt(f.board.ID, 10) + "/qa"
	resp, body := f.s.req(t, "GET", bp, nil, ck)
	var v qaView
	json.Unmarshal(body, &v)
	if resp.StatusCode != 200 || !v.Config.Configured() || !v.Toolchain || v.Session.State != "valid" {
		t.Fatalf("get: %d %s", resp.StatusCode, body)
	}
	if resp, _ := f.s.req(t, "GET", bp, nil, nil); resp.StatusCode != 401 {
		t.Errorf("no session: %d", resp.StatusCode)
	}
	bad := v.Config
	bad.Services = []qa.Service{{Name: "web", Folder: t.TempDir(), Command: "x", Port: 5000, ReadyURL: "http://localhost:5000/"}}
	if resp, body := f.s.req(t, "PUT", bp, bad, ck); resp.StatusCode != 400 {
		t.Errorf("folder outside the board: %d %s", resp.StatusCode, body)
	}
	v.Config.Headed = true
	if resp, body := f.s.req(t, "PUT", bp, v.Config, ck); resp.StatusCode != 200 || !strings.Contains(string(body), `"headed":true`) {
		t.Errorf("put: %d %s", resp.StatusCode, body)
	}
	if resp, _ := f.s.req(t, "GET", "/api/boards/999/qa", nil, ck); resp.StatusCode != 404 {
		t.Errorf("unknown board: %d", resp.StatusCode)
	}
	// forget login
	if resp, _ := f.s.req(t, "DELETE", bp+"/session", nil, ck); resp.StatusCode != 204 {
		t.Errorf("forget: %d", resp.StatusCode)
	}
	if _, err := os.Stat(f.p.Auth(f.board.ID)); err == nil {
		t.Error("login file should be gone")
	}
	_, body = f.s.req(t, "POST", bp+"/session/check", map[string]any{}, ck)
	if !strings.Contains(string(body), `"state":"none"`) {
		t.Errorf("check = %s", body)
	}
}

func TestQALoginRoute(t *testing.T) {
	f := qaFixture(t)
	ck := f.s.login(t)
	bp := "/api/boards/" + strconv.FormatInt(f.board.ID, 10) + "/qa"
	var gotURL string
	done := make(chan struct{})
	var once sync.Once
	old := qaLogin
	t.Cleanup(func() { qaLogin = old })
	qaLogin = func(_ context.Context, p qa.Paths, id int64, u string) error {
		gotURL = u
		os.WriteFile(p.Auth(id), []byte(`{"cookies":[{"name":"s","value":"abcdefgh12345"}]}`), 0o600)
		once.Do(func() { close(done) })
		return nil
	}
	if resp, body := f.s.req(t, "POST", bp+"/login", map[string]any{}, ck); resp.StatusCode != 202 {
		t.Fatalf("login: %d %s", resp.StatusCode, body)
	}
	<-done
	if gotURL != "http://localhost:5000" {
		t.Errorf("login url = %q", gotURL)
	}
	for i := 0; i < 100 && f.s.St != nil; i++ { // the lock is released when the script ends
		if resp, _ := f.s.req(t, "POST", bp+"/login", map[string]any{}, ck); resp.StatusCode == 202 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("the lock was never released")
}

func TestQAEvidenceRoute(t *testing.T) {
	f := qaFixture(t)
	f.stub(t, session{class: "passed", verdict: passJSON, read: true})
	_, run := f.run(t)
	ck := f.s.login(t)
	r := strconv.FormatInt(run.ID, 10)
	os.WriteFile(filepath.Join(f.p.Attempt(run.ID, 1), "trace.zip"), []byte("zip"), 0o644)
	os.MkdirAll(filepath.Join(f.p.Run(run.ID), "services"), 0o755)
	os.WriteFile(filepath.Join(f.p.Run(run.ID), "services", "web.log"), []byte("secret log"), 0o644)

	resp, body := f.s.req(t, "GET", "/api/qa-evidence/"+r+"/1/steps/01-open.png", nil, ck)
	if resp.StatusCode != 200 || string(body) != "png" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("png: %d %q %v", resp.StatusCode, body, resp.Header)
	}
	if resp, _ := f.s.req(t, "GET", "/api/qa-evidence/"+r+"/1/trace.zip", nil, ck); resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("trace: %d %v", resp.StatusCode, resp.Header)
	}
	if resp, _ := f.s.req(t, "GET", "/api/qa-evidence/"+r+"/0/repos.json", nil, ck); resp.StatusCode != 200 {
		t.Errorf("repos.json: %d", resp.StatusCode)
	}
	if resp, _ := f.s.req(t, "GET", "/api/qa-evidence/"+r+"/1/steps/01-open.png", nil, nil); resp.StatusCode != 401 {
		t.Errorf("no session: %d", resp.StatusCode)
	}
	for _, bad := range []string{
		"/1/steps/%2e%2e%2f%2e%2e%2ftrace.zip", "/1/%2e%2e/%2e%2e/qa/auth/board-1.json", "/0/services/web.log", "/1/auth/board-1.json",
		"/1/sandbox.sb", "/1/class.json", "/x/steps/01-open.png", "/1/steps/01-open.svg", "/-1/repos.json", "/9/steps/01-open.png", "/1/steps/99-nothing.png",
	} {
		if resp, body := f.s.req(t, "GET", "/api/qa-evidence/"+r+bad, nil, ck); resp.StatusCode == http.StatusOK {
			t.Errorf("%s was served: %q", bad, body)
		}
	}
	if resp, _ := f.s.req(t, "GET", "/api/qa-evidence/9999/1/steps/01-open.png", nil, ck); resp.StatusCode != 404 {
		t.Errorf("unknown run: %d", resp.StatusCode)
	}
	// the runs list for the item
	_, body = f.s.req(t, "GET", f.ip+"/qa-runs", nil, ck)
	if !strings.Contains(string(body), `"status":"passed"`) || !strings.Contains(string(body), "01-open.png") {
		t.Errorf("qa-runs = %s", body)
	}
}

func TestQASweepRemovesOrphans(t *testing.T) {
	f := qaFixture(t)
	f.stub(t, session{class: "passed", verdict: passJSON, read: true})
	_, run := f.run(t)
	orphan := f.p.Run(9999)
	os.MkdirAll(orphan, 0o755)
	sweepQA(f.s.St)
	if _, err := os.Stat(orphan); err == nil {
		t.Error("orphan run folder should be removed")
	}
	if _, err := os.Stat(f.p.Run(run.ID)); err != nil {
		t.Error("a live run's folder must stay")
	}
}
