package api

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestGenerateDescription(t *testing.T) {
	s := newTestServer(t)
	c := s.login(t)
	defer func(f func(context.Context, string, string) (string, error)) { runClaude = f }(runClaude)

	var gotSystem, gotUser string
	runClaude = func(_ context.Context, system, user string) (string, error) {
		gotSystem, gotUser = system, user
		return "## Summary\nDone.\n", nil
	}

	_, body := s.req(t, "POST", "/api/boards", map[string]any{"name": "Harbor App", "description": "Shipping tracker"}, c)
	boardID := jsonInt(t, body, "id")

	resp, body := s.req(t, "POST", "/api/generate-description",
		map[string]any{"prompt": "let ops save filters", "title": "Saved searches", "boardId": boardID}, c)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"description":"## Summary\nDone."`) {
		t.Fatalf("generate: %d %s", resp.StatusCode, body)
	}
	for _, want := range []string{"let ops save filters", "Saved searches", "Harbor App", "Shipping tracker"} {
		if !strings.Contains(gotUser, want) {
			t.Errorf("user message missing %q:\n%s", want, gotUser)
		}
	}
	if !strings.Contains(strings.ToLower(gotSystem), "definition of done") {
		t.Errorf("system prompt should ask for a Definition of Done:\n%s", gotSystem)
	}
}

func TestGenerateDescriptionValidationAndFailure(t *testing.T) {
	s := newTestServer(t)
	c := s.login(t)
	defer func(f func(context.Context, string, string) (string, error)) { runClaude = f }(runClaude)
	runClaude = func(context.Context, string, string) (string, error) { return "x", nil }

	post := func(m map[string]any, cookie bool) int {
		ck := c
		if !cookie {
			ck = nil
		}
		resp, _ := s.req(t, "POST", "/api/generate-description", m, ck)
		return resp.StatusCode
	}
	if got := post(map[string]any{"prompt": "   "}, true); got != 400 {
		t.Errorf("blank prompt: %d, want 400", got)
	}
	if got := post(map[string]any{"prompt": strings.Repeat("a", 2001)}, true); got != 400 {
		t.Errorf("oversized prompt: %d, want 400", got)
	}
	if got := post(map[string]any{"prompt": "x", "boardId": 9999}, true); got != 404 {
		t.Errorf("unknown board: %d, want 404", got)
	}
	if got := post(map[string]any{"prompt": "x"}, false); got != 401 {
		t.Errorf("unauthenticated: %d, want 401", got)
	}
	runClaude = func(context.Context, string, string) (string, error) { return "", errors.New("exit 1") }
	if got := post(map[string]any{"prompt": "x"}, true); got != 502 {
		t.Errorf("claude failure: %d, want 502", got)
	}
	runClaude = func(context.Context, string, string) (string, error) { return "  \n", nil }
	if got := post(map[string]any{"prompt": "x"}, true); got != 502 {
		t.Errorf("empty output: %d, want 502", got)
	}
}

func jsonInt(t *testing.T, body []byte, key string) int {
	t.Helper()
	s := string(body)
	i := strings.Index(s, `"`+key+`":`)
	if i < 0 {
		t.Fatalf("no %q in %s", key, s)
	}
	rest := s[i+len(key)+3:]
	j := strings.IndexAny(rest, ",}")
	n, err := strconv.Atoi(rest[:j])
	if err != nil {
		t.Fatal(err)
	}
	return n
}
