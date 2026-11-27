package api

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

const itemsReply = `{"split":false,"reason":"","agent_tasks":[{"title":"Add login endpoint","body":"POST /api/login"},{"title":"Build login form","body":"Form"}],"qa_cases":[{"title":"Wrong password is rejected","body":"Expect an error"}]}`

func TestGenerateItemsNeedsApprovalThenFillsLists(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Let the owner sign in"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	wait := reviewWaiter(t, s, cp, ck)
	wait("done")

	var gotUser string
	defer func(f func(context.Context, string, string) (string, error)) { runClaude = f }(runClaude)
	runClaude = func(_ context.Context, system, user string) (string, error) {
		gotUser = user
		return "```json\n" + itemsReply + "\n```", nil
	}

	if resp, _ := s.req(t, "POST", cp+"/generate-items", map[string]any{}, ck); resp.StatusCode != 409 {
		t.Fatalf("generate before approval: %d, want 409", resp.StatusCode)
	}
	if resp, _ := s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck); resp.StatusCode != 200 {
		t.Fatalf("approve: %d", resp.StatusCode)
	}
	code, job := s.gen(t, cp+"/generate-items", map[string]any{}, ck)
	var out struct {
		Split bool         `json:"split"`
		Items []store.Item `json:"items"`
	}
	out.Items, _ = s.St.ListItems(card.ID)
	if code != 202 || job.Status != "done" || !job.Consumed || len(out.Items) != 2 {
		t.Fatalf("generate: %d %+v", code, job)
	}
	if !strings.Contains(gotUser, "Let the owner sign in") {
		t.Errorf("prompt should carry the ticket:\n%s", gotUser)
	}
	if out.Items[0].Kind != "agent" || len(out.Items[0].Log) == 0 || out.Items[1].Kind != "agent" {
		t.Fatalf("items = %+v", out.Items)
	}

	// QA Test Cases are generated only once the ticket is in the QA column, and keep the Agent Tasks
	if resp, _ := s.req(t, "POST", cp+"/generate-items", map[string]any{"kind": "qa"}, ck); resp.StatusCode != 409 {
		t.Fatalf("generate qa before the QA column: %d, want 409", resp.StatusCode)
	}
	if resp, body := s.req(t, "POST", cp+"/move", map[string]any{"status": "qa", "position": 0}, ck); resp.StatusCode != 200 {
		t.Fatalf("move to qa: %d %s", resp.StatusCode, body)
	}
	code, job = s.gen(t, cp+"/generate-items", map[string]any{"kind": "qa"}, ck)
	allNow, _ := s.St.ListItems(card.ID)
	qaCount := 0
	for _, x := range allNow {
		if x.Kind == "qa" {
			qaCount++
		}
	}
	if code != 202 || job.Status != "done" || qaCount != 1 {
		t.Fatalf("generate qa: %d %+v", code, job)
	}
	if !strings.Contains(gotUser, "Add login endpoint") {
		t.Errorf("qa prompt should carry the agent tasks:\n%s", gotUser)
	}
	if all, _ := s.St.ListItems(card.ID); len(all) != 3 {
		t.Fatalf("generating QA must keep the Agent Tasks: %+v", all)
	}

	// edit, status, technical flow
	ip := "/api/items/" + strconv.FormatInt(out.Items[0].ID, 10)
	resp, body := s.req(t, "PUT", ip, map[string]any{"title": "Add login API", "body": "x", "status": "done"}, ck)
	var it store.Item
	json.Unmarshal(body, &it)
	if resp.StatusCode != 200 || it.Title != "Add login API" || it.Status != "done" {
		t.Fatalf("update item: %d %s", resp.StatusCode, body)
	}
	// a stale form cannot mark an item In Progress when no run is working on it
	if _, body = s.req(t, "PUT", ip, map[string]any{"title": "Add login API", "body": "x", "status": "doing"}, ck); !strings.Contains(string(body), `"status":"done"`) {
		t.Fatalf("stale doing accepted: %s", body)
	}
	if resp, body = s.req(t, "POST", ip+"/flows", map[string]any{"title": "Check password", "source": "A -> B"}, ck); resp.StatusCode != 201 {
		t.Fatalf("tech flow: %d %s", resp.StatusCode, body)
	}
	var f store.Flow
	json.Unmarshal(body, &f)
	s.req(t, "PUT", "/api/flows/"+strconv.FormatInt(f.ID, 10), map[string]any{"title": "Check", "source": "A -> B"}, ck)
	_, body = s.req(t, "GET", ip+"/flows", nil, ck)
	var fs []store.Flow
	json.Unmarshal(body, &fs)
	if len(fs) != 1 || fs[0].Title != "Check" {
		t.Fatalf("technical flows = %s", body)
	}
	if c, _ := s.St.GetCard(card.ID); !c.Approved {
		t.Fatal("editing tasks and technical flows must not revoke the ticket approval")
	}
	if resp, _ = s.req(t, "DELETE", ip, nil, ck); resp.StatusCode != 204 {
		t.Fatalf("delete item: %d", resp.StatusCode)
	}
	_, body = s.req(t, "GET", cp+"/items", nil, ck)
	var items []store.Item
	json.Unmarshal(body, &items)
	if len(items) != 2 {
		t.Fatalf("items after delete = %s", body)
	}
}

func TestGenerateItemsSplitAndFailure(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Whole app"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	reviewWaiter(t, s, cp, ck)("done")
	s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck)
	s.St.CreateItem(card.ID, "agent", "existing", "")

	defer func(f func(context.Context, string, string) (string, error)) { runClaude = f }(runClaude)
	runClaude = func(context.Context, string, string) (string, error) {
		return `{"split":true,"reason":"Covers auth and billing","agent_tasks":[],"qa_cases":[]}`, nil
	}
	code, job := s.gen(t, cp+"/generate-items", map[string]any{}, ck)
	var out struct {
		Split  bool   `json:"split"`
		Reason string `json:"reason"`
	}
	json.Unmarshal([]byte(job.Result), &out)
	if code != 202 || !out.Split || out.Reason != "Covers auth and billing" {
		t.Fatalf("split: %d %+v", code, job)
	}
	if items, _ := s.St.ListItems(card.ID); len(items) != 1 {
		t.Fatalf("a split verdict must keep the existing items: %+v", items)
	}
	runClaude = func(context.Context, string, string) (string, error) { return "not json", nil }
	if _, job = s.gen(t, cp+"/generate-items", map[string]any{}, ck); job.Status != "error" {
		t.Fatalf("unreadable reply: job %q, want error", job.Status)
	}
}

func TestGeneratedItemsRememberTicketVersion(t *testing.T) {
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
	defer func(f func(context.Context, string, string) (string, error)) { runClaude = f }(runClaude)
	runClaude = func(context.Context, string, string) (string, error) { return itemsReply, nil }
	s.gen(t, cp+"/generate-items", map[string]any{}, ck)
	var out struct{ Items []store.Item }
	out.Items, _ = s.St.ListItems(card.ID)
	if len(out.Items) == 0 || out.Items[0].Version != card.ContentVersion || card.ContentVersion == 0 {
		t.Fatalf("items = %+v, card version %d", out.Items, card.ContentVersion)
	}
}

func TestGenerateTasksAlsoDrawsTechnicalFlows(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Let the owner sign in"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	reviewWaiter(t, s, cp, ck)("done")
	defer func(f func(context.Context, string, string) (string, error)) { runClaude = f }(runClaude)
	runClaude = func(context.Context, string, string) (string, error) { return "```json\n" + itemsReply + "\n```", nil }
	defer func(f func(context.Context, string, string, []string, func(string)) (string, error)) {
		streamClaude = f
	}(streamClaude)
	var calls int32
	streamClaude = func(_ context.Context, system, user string, _ []string, _ func(string)) (string, error) {
		atomic.AddInt32(&calls, 1)
		if !strings.Contains(system, "Technical Flow") || !strings.Contains(system, "real files") {
			t.Errorf("not the code-reading technical prompt: %s", system)
		}
		return `{"title":"Log activity","source":"title: Log activity\nautonumber\nHook -> Api: POST /logs"}`, nil
	}
	s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck)
	if code, job := s.gen(t, cp+"/generate-items", map[string]any{}, ck); code != 202 || job.Status != "done" {
		t.Fatalf("generate: %d %+v", code, job)
	}
	items, _ := s.St.ListItems(card.ID)
	if len(items) != 2 || atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("items=%d flow calls=%d", len(items), calls)
	}
	for _, it := range items {
		if fl, _ := s.St.ListItemFlows(it.ID); len(fl) != 1 || fl[0].Title != "Log activity" {
			t.Errorf("task %s flows = %+v", it.Ident, fl)
		}
	}
}

func TestGenerateQAReadsTheCodeAndAsksForPrerequisites(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	folder := t.TempDir()
	resp, bb := s.req(t, "POST", "/api/boards", map[string]any{"name": "B", "folders": []string{folder}}, ck)
	if resp.StatusCode != 201 {
		t.Fatalf("board: %d %s", resp.StatusCode, bb)
	}
	var board store.Board
	json.Unmarshal(bb, &board)
	bp := "/api/boards/" + strconv.FormatInt(board.ID, 10)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Let the owner sign in"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	reviewWaiter(t, s, cp, ck)("done")
	s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck)
	s.St.DB.Exec(`UPDATE cards SET status='qa' WHERE id=?`, card.ID)
	defer func(f func(context.Context, string, string, []string, func(string)) (string, error)) {
		streamClaude = f
	}(streamClaude)
	var gotSystem string
	var gotDirs []string
	streamClaude = func(_ context.Context, system, _ string, dirs []string, _ func(string)) (string, error) {
		gotSystem, gotDirs = system, dirs
		return `{"qa_cases":[{"title":"Sign in","body":"## Prerequisites\n- Login: no login\n## Steps\n1. open"}]}`, nil
	}
	if code, job := s.gen(t, cp+"/generate-items", map[string]any{"kind": "qa"}, ck); code != 202 || job.Status != "done" {
		t.Fatalf("generate: %d %+v", code, job)
	}
	if !strings.Contains(gotSystem, "## Prerequisites") || !strings.Contains(gotSystem, "Read, Grep, Glob") {
		t.Errorf("the prompt must ask for Prerequisites and allow reading code:\n%s", gotSystem)
	}
	if len(gotDirs) == 0 {
		t.Error("Claude must be given the board folders to read")
	}
}
