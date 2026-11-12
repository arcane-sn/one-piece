package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func TestGenerateFlowBusinessAndTechnical(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Let the owner sign in"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	item, err := s.St.CreateItem(card.ID, "agent", "Add login endpoint", "POST /api/login")
	if err != nil {
		t.Fatal(err)
	}

	var gotSystem, gotUser string
	defer func(f func(context.Context, string, string) (string, error)) { runClaude = f }(runClaude)
	runClaude = func(_ context.Context, system, user string) (string, error) {
		gotSystem, gotUser = system, user
		return "```json\n" + `{"title":"Owner signs in","source":"title: Owner signs in\nautonumber\nOwner -> App: Opens login\nApp --> Owner: Shows form"}` + "\n```", nil
	}
	resp, body := s.req(t, "POST", "/api/flows/generate", map[string]any{"kind": "business", "id": card.ID, "prompt": "happy path and wrong password"}, ck)
	var out struct{ Title, Source string }
	json.Unmarshal(body, &out)
	if resp.StatusCode != 200 || out.Title != "Owner signs in" || !strings.HasPrefix(out.Source, "title: Owner signs in\nautonumber\nOwner -> App") {
		t.Fatalf("business: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(gotSystem, "Business Flow") || !strings.Contains(gotUser, "Let the owner sign in") || !strings.Contains(gotUser, "wrong password") {
		t.Errorf("business prompt:\n%s\n%s", gotSystem, gotUser)
	}

	resp, body = s.req(t, "POST", "/api/flows/generate", map[string]any{"kind": "technical", "id": item.ID, "prompt": "api only",
		"previous": "bad line", "problems": []int{1}}, ck)
	if resp.StatusCode != 200 || !strings.Contains(gotSystem, "Technical Flow") || !strings.Contains(gotUser, "POST /api/login") ||
		!strings.Contains(gotUser, "line numbers [1]") {
		t.Fatalf("technical: %d %s\n%s\n%s", resp.StatusCode, body, gotSystem, gotUser)
	}

	// bare swimlane text still works; prose without arrows is rejected
	runClaude = func(context.Context, string, string) (string, error) {
		return "title: Bare\nA -> B: hi", nil
	}
	_, body = s.req(t, "POST", "/api/flows/generate", map[string]any{"kind": "business", "id": card.ID, "prompt": "x"}, ck)
	json.Unmarshal(body, &out)
	if out.Title != "Bare" {
		t.Fatalf("bare text: %s", body)
	}
	runClaude = func(context.Context, string, string) (string, error) { return "I cannot do that", nil }
	if resp, _ = s.req(t, "POST", "/api/flows/generate", map[string]any{"kind": "business", "id": card.ID, "prompt": "x"}, ck); resp.StatusCode != 502 {
		t.Fatalf("prose reply: %d, want 502", resp.StatusCode)
	}
	if resp, _ = s.req(t, "POST", "/api/flows/generate", map[string]any{"kind": "business", "id": card.ID, "prompt": ""}, ck); resp.StatusCode != 400 {
		t.Fatalf("empty prompt: %d, want 400", resp.StatusCode)
	}
}
