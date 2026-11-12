package api

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func TestBoardKeyEditable(t *testing.T) {
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	resp, body := s.req(t, "PUT", bp, map[string]any{"name": "B", "key": "abc"}, ck)
	var b store.Board
	json.Unmarshal(body, &b)
	if resp.StatusCode != 200 || b.Key != "ABC" {
		t.Fatalf("set key: %d %s", resp.StatusCode, body)
	}
	if resp, _ := s.req(t, "PUT", bp, map[string]any{"name": "B", "key": "a"}, ck); resp.StatusCode != 400 {
		t.Fatalf("short key: %d, want 400", resp.StatusCode)
	}
	if resp, _ := s.req(t, "POST", "/api/boards", map[string]any{"name": "Other"}, ck); resp.StatusCode != 201 {
		t.Fatal("create other")
	}
	_, body = s.req(t, "GET", "/api/boards", nil, ck)
	var all []store.Board
	json.Unmarshal(body, &all)
	op := "/api/boards/" + strconv.FormatInt(all[1].ID, 10)
	if resp, _ := s.req(t, "PUT", op, map[string]any{"name": "Other", "key": "ABC"}, ck); resp.StatusCode != 400 {
		t.Fatalf("taken key: %d, want 400", resp.StatusCode)
	}
	// omitting the key leaves it alone
	resp, body = s.req(t, "PUT", bp, map[string]any{"name": "B2"}, ck)
	json.Unmarshal(body, &b)
	if resp.StatusCode != 200 || b.Key != "ABC" {
		t.Fatalf("no key: %d %s", resp.StatusCode, body)
	}
}

func TestPromptsCarryIdentifiers(t *testing.T) {
	stub := useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	s.req(t, "PUT", bp, map[string]any{"name": "B", "folders": []string{t.TempDir()}}, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Sign in"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	reviewWaiter(t, s, cp, ck)("done")
	stub.mu.Lock()
	review := stub.user
	stub.mu.Unlock()
	if !strings.Contains(review, "Ticket ID: "+card.Ident+"\n") {
		t.Errorf("review prompt lacks ident %q:\n%s", card.Ident, review)
	}
	s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck)

	var itemsUser string
	defer func(f func(context.Context, string, string) (string, error)) { runClaude = f }(runClaude)
	runClaude = func(_ context.Context, _, user string) (string, error) {
		itemsUser = user
		return `{"split":false,"agent_tasks":[{"title":"T","body":"b"}]}`, nil
	}
	if code, job := s.gen(t, cp+"/generate-items", map[string]any{"kind": "agent"}, ck); code != 202 || job.Status != "done" {
		t.Fatalf("generate: %d %+v", code, job)
	}
	if !strings.Contains(itemsUser, "Ticket ID: "+card.Ident+"\n") {
		t.Errorf("items prompt lacks ident:\n%s", itemsUser)
	}

	_, body = s.req(t, "GET", cp+"/items", nil, ck)
	var items []store.Item
	json.Unmarshal(body, &items)
	task := items[0]
	_, body = s.req(t, "POST", cp+"/items", map[string]any{"kind": "qa", "title": "Q", "body": "steps"}, ck)
	var qa store.Item
	json.Unmarshal(body, &qa)

	var runUser string
	defer func(f func(context.Context, string, []string, string, string, func(string)) (string, error)) {
		agentRun = f
	}(agentRun)
	agentRun = func(_ context.Context, _ string, _ []string, _, user string, _ func(string)) (string, error) {
		runUser = user
		return "ok", nil
	}
	ip := "/api/items/" + strconv.FormatInt(task.ID, 10)
	if resp, body := s.req(t, "POST", ip+"/run", map[string]any{}, ck); resp.StatusCode != 202 {
		t.Fatalf("run: %d %s", resp.StatusCode, body)
	}
	waitItem(t, s, cp, ip, ck, "done")
	if !strings.Contains(runUser, "Your Agent Task: "+task.Ident+" ") {
		t.Errorf("run prompt lacks task ident %q:\n%s", task.Ident, runUser)
	}

	stub.set(`{"result":"pass","issue":"","desc":"ok"}`, nil)
	qp := "/api/items/" + strconv.FormatInt(qa.ID, 10)
	if resp, body := s.req(t, "POST", qp+"/run", map[string]any{}, ck); resp.StatusCode != 202 {
		t.Fatalf("qa run: %d %s", resp.StatusCode, body)
	}
	waitItem(t, s, cp, qp, ck, "done")
	time.Sleep(20 * time.Millisecond)
	stub.mu.Lock()
	qaUser := stub.user
	stub.mu.Unlock()
	if !strings.Contains(qaUser, "Your QA Test Case: "+qa.Ident+" ") {
		t.Errorf("qa prompt lacks ident %q:\n%s", qa.Ident, qaUser)
	}
}
