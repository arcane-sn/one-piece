package api

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
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
	resp, body := s.req(t, "POST", cp+"/generate-items", map[string]any{}, ck)
	var out struct {
		Split bool         `json:"split"`
		Items []store.Item `json:"items"`
	}
	json.Unmarshal(body, &out)
	if resp.StatusCode != 200 || out.Split || len(out.Items) != 3 {
		t.Fatalf("generate: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(gotUser, "Let the owner sign in") {
		t.Errorf("prompt should carry the ticket:\n%s", gotUser)
	}
	if out.Items[0].Kind != "agent" || len(out.Items[0].Log) == 0 || out.Items[2].Kind != "qa" {
		t.Fatalf("items = %+v", out.Items)
	}

	// edit, status, technical flow
	ip := "/api/items/" + strconv.FormatInt(out.Items[0].ID, 10)
	resp, body = s.req(t, "PUT", ip, map[string]any{"title": "Add login API", "body": "x", "status": "doing"}, ck)
	var it store.Item
	json.Unmarshal(body, &it)
	if resp.StatusCode != 200 || it.Title != "Add login API" || it.Status != "doing" {
		t.Fatalf("update item: %d %s", resp.StatusCode, body)
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
	resp, body := s.req(t, "POST", cp+"/generate-items", map[string]any{}, ck)
	var out struct {
		Split  bool   `json:"split"`
		Reason string `json:"reason"`
	}
	json.Unmarshal(body, &out)
	if resp.StatusCode != 200 || !out.Split || out.Reason != "Covers auth and billing" {
		t.Fatalf("split: %d %s", resp.StatusCode, body)
	}
	if items, _ := s.St.ListItems(card.ID); len(items) != 1 {
		t.Fatalf("a split verdict must keep the existing items: %+v", items)
	}
	runClaude = func(context.Context, string, string) (string, error) { return "not json", nil }
	if resp, _ = s.req(t, "POST", cp+"/generate-items", map[string]any{}, ck); resp.StatusCode != 502 {
		t.Fatalf("unreadable reply: %d, want 502", resp.StatusCode)
	}
}
