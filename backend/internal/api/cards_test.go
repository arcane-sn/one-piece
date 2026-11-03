package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func newBoardPath(t *testing.T, s *testSrv, c *http.Cookie) string {
	t.Helper()
	resp, body := s.req(t, "POST", "/api/boards", map[string]any{"name": "B"}, c)
	if resp.StatusCode != 201 {
		t.Fatalf("create board: %d %s", resp.StatusCode, body)
	}
	var b store.Board
	json.Unmarshal(body, &b)
	return "/api/boards/" + strconv.FormatInt(b.ID, 10)
}

func TestCardFlow(t *testing.T) {
	s := newTestServer(t)
	c := s.login(t)
	bp := newBoardPath(t, s, c)

	resp, body := s.req(t, "POST", bp+"/cards", map[string]string{"title": "T1", "description": "d"}, c)
	if resp.StatusCode != 201 {
		t.Fatalf("create card: %d %s", resp.StatusCode, body)
	}
	var card store.Card
	json.Unmarshal(body, &card)
	if card.Status != "business" {
		t.Fatalf("new card status = %q", card.Status)
	}
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)

	if resp, body = s.req(t, "PUT", cp, map[string]string{"title": "T2", "description": ""}, c); resp.StatusCode != 200 {
		t.Fatalf("update: %d %s", resp.StatusCode, body)
	}
	if resp, body = s.req(t, "POST", cp+"/move", map[string]any{"status": "done", "position": 0}, c); resp.StatusCode != 200 {
		t.Fatalf("move: %d %s", resp.StatusCode, body)
	}
	_, body = s.req(t, "GET", bp+"/cards", nil, c)
	var list []store.Card
	json.Unmarshal(body, &list)
	if len(list) != 1 || list[0].Status != "done" || list[0].Title != "T2" {
		t.Fatalf("list = %s", body)
	}
	if resp, _ = s.req(t, "DELETE", cp, nil, c); resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
}

func TestMoveBadStatusIs400(t *testing.T) {
	s := newTestServer(t)
	c := s.login(t)
	bp := newBoardPath(t, s, c)
	_, body := s.req(t, "POST", bp+"/cards", map[string]string{"title": "T"}, c)
	var card store.Card
	json.Unmarshal(body, &card)
	resp, _ := s.req(t, "POST", "/api/cards/"+strconv.FormatInt(card.ID, 10)+"/move", map[string]any{"status": "archived", "position": 0}, c)
	if resp.StatusCode != 400 {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
}

func TestCardOnMissingBoardIs404(t *testing.T) {
	s := newTestServer(t)
	c := s.login(t)
	if resp, _ := s.req(t, "GET", "/api/boards/9999/cards", nil, c); resp.StatusCode != 404 {
		t.Errorf("list: %d, want 404", resp.StatusCode)
	}
	if resp, _ := s.req(t, "POST", "/api/boards/9999/cards", map[string]string{"title": "x"}, c); resp.StatusCode != 404 {
		t.Errorf("create: %d, want 404", resp.StatusCode)
	}
}

func TestCardDeadlineOverAPI(t *testing.T) {
	s := newTestServer(t)
	c := s.login(t)
	bp := newBoardPath(t, s, c)

	resp, body := s.req(t, "POST", bp+"/cards", map[string]string{"title": "x", "deadline": "2026-12-31"}, c)
	var card store.Card
	json.Unmarshal(body, &card)
	if resp.StatusCode != 201 || card.Deadline != "2026-12-31" {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	if resp, _ = s.req(t, "PUT", cp, map[string]string{"title": "x", "deadline": "nope"}, c); resp.StatusCode != 400 {
		t.Fatalf("bad deadline should be 400, got %d", resp.StatusCode)
	}
	if resp, _ = s.req(t, "POST", bp+"/cards", map[string]string{"title": "y", "deadline": "nope"}, c); resp.StatusCode != 400 {
		t.Fatalf("bad deadline on create should be 400, got %d", resp.StatusCode)
	}
}
