package api

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func TestBoardCRUD(t *testing.T) {
	s := newTestServer(t)
	c := s.login(t)
	dir := t.TempDir()

	resp, body := s.req(t, "POST", "/api/boards", map[string]any{"name": "Shop", "folders": []string{dir}}, c)
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	var b store.Board
	json.Unmarshal(body, &b)
	if b.Name != "Shop" || len(b.Folders) != 1 {
		t.Fatalf("created = %+v", b)
	}

	_, body = s.req(t, "GET", "/api/boards", nil, c)
	var list []store.Board
	json.Unmarshal(body, &list)
	if len(list) != 1 {
		t.Fatalf("list = %s", body)
	}

	path := "/api/boards/" + strconv.FormatInt(b.ID, 10)
	resp, body = s.req(t, "PUT", path, map[string]any{"name": "Shop2", "folders": []string{}}, c)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"folders":[]`) {
		t.Fatalf("update: %d %s", resp.StatusCode, body)
	}
	if resp, _ = s.req(t, "GET", path, nil, c); resp.StatusCode != 200 {
		t.Fatalf("get: %d", resp.StatusCode)
	}
	if resp, _ = s.req(t, "DELETE", path, nil, c); resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp, _ = s.req(t, "GET", path, nil, c); resp.StatusCode != 404 {
		t.Fatalf("get after delete: %d", resp.StatusCode)
	}
}

func TestListBoardsEmptyIsArray(t *testing.T) {
	s := newTestServer(t)
	_, body := s.req(t, "GET", "/api/boards", nil, s.login(t))
	if strings.TrimSpace(string(body)) != "[]" {
		t.Errorf("body = %q, want []", body)
	}
}

func TestBoardBadIDIs404(t *testing.T) {
	s := newTestServer(t)
	c := s.login(t)
	for _, p := range []string{"/api/boards/abc", "/api/boards/9999"} {
		if resp, _ := s.req(t, "GET", p, nil, c); resp.StatusCode != 404 {
			t.Errorf("%s: %d, want 404", p, resp.StatusCode)
		}
	}
}

func TestBoardBadFolderIs400(t *testing.T) {
	s := newTestServer(t)
	resp, _ := s.req(t, "POST", "/api/boards", map[string]any{"name": "x", "folders": []string{"relative"}}, s.login(t))
	if resp.StatusCode != 400 {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
}
