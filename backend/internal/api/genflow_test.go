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
	code, job := s.gen(t, "/api/flows/generate", map[string]any{"kind": "business", "id": card.ID, "prompt": "happy path and wrong password"}, ck)
	var out struct{ Title, Source string }
	json.Unmarshal([]byte(job.Result), &out)
	if code != 202 || job.Status != "done" || out.Title != "Owner signs in" || !strings.HasPrefix(out.Source, "title: Owner signs in\nautonumber\nOwner -> App") {
		t.Fatalf("business: %d %+v", code, job)
	}
	if !strings.Contains(gotSystem, "Business Flow") || !strings.Contains(gotUser, "Let the owner sign in") || !strings.Contains(gotUser, "wrong password") {
		t.Errorf("business prompt:\n%s\n%s", gotSystem, gotUser)
	}

	code, job = s.gen(t, "/api/flows/generate", map[string]any{"kind": "technical", "id": item.ID, "prompt": "api only",
		"previous": "bad line", "problems": []int{1}}, ck)
	if code != 202 || job.Status != "done" || job.Scope != "item" || !strings.Contains(gotSystem, "Technical Flow") || !strings.Contains(gotUser, "POST /api/login") ||
		!strings.Contains(gotUser, "line numbers [1]") {
		t.Fatalf("technical: %d %+v\n%s\n%s", code, job, gotSystem, gotUser)
	}

	// bare swimlane text still works; prose without arrows is rejected
	runClaude = func(context.Context, string, string) (string, error) {
		return "title: Bare\nA -> B: hi", nil
	}
	_, job = s.gen(t, "/api/flows/generate", map[string]any{"kind": "business", "id": card.ID, "prompt": "x"}, ck)
	json.Unmarshal([]byte(job.Result), &out)
	if out.Title != "Bare" {
		t.Fatalf("bare text: %+v", job)
	}
	runClaude = func(context.Context, string, string) (string, error) { return "I cannot do that", nil }
	if _, job = s.gen(t, "/api/flows/generate", map[string]any{"kind": "business", "id": card.ID, "prompt": "x"}, ck); job.Status != "error" {
		t.Fatalf("prose reply: job %q, want error", job.Status)
	}
	if code, _ = s.gen(t, "/api/flows/generate", map[string]any{"kind": "business", "id": card.ID, "prompt": ""}, ck); code != 400 {
		t.Fatalf("empty prompt: %d, want 400", code)
	}
}
