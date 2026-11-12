package api

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func TestFlowsAPI(t *testing.T) {
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)

	resp, body := s.req(t, "POST", cp+"/flows", map[string]any{"title": "Sign in", "source": "User -> App: opens login"}, ck)
	var f store.Flow
	json.Unmarshal(body, &f)
	if resp.StatusCode != 201 || f.ID == 0 || f.Title != "Sign in" {
		t.Fatalf("create flow: %d %s", resp.StatusCode, body)
	}
	fp := "/api/flows/" + strconv.FormatInt(f.ID, 10)
	if resp, body = s.req(t, "PUT", fp, map[string]any{"title": "Sign in 2", "source": "A -> B"}, ck); resp.StatusCode != 200 {
		t.Fatalf("update: %d %s", resp.StatusCode, body)
	}
	_, body = s.req(t, "GET", cp+"/flows", nil, ck)
	var fs []store.Flow
	json.Unmarshal(body, &fs)
	if len(fs) != 1 || fs[0].Title != "Sign in 2" {
		t.Fatalf("list = %s", body)
	}
	if resp, _ = s.req(t, "DELETE", fp, nil, ck); resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp, _ = s.req(t, "DELETE", fp, nil, ck); resp.StatusCode != 404 {
		t.Fatalf("second delete: %d, want 404", resp.StatusCode)
	}
	if resp, _ = s.req(t, "POST", cp+"/flows", map[string]any{"title": ""}, ck); resp.StatusCode != 400 {
		t.Fatalf("empty title: %d, want 400", resp.StatusCode)
	}
	if resp, _ = s.req(t, "GET", "/api/cards/9999/flows", nil, ck); resp.StatusCode != 404 {
		t.Fatalf("missing card: %d, want 404", resp.StatusCode)
	}
}
