package api

import (
	"errors"
	"strings"
	"testing"
)

func TestPickFolder(t *testing.T) {
	s := newTestServer(t)
	c := s.login(t)
	defer func(f func() (string, error)) { pickFolder = f }(pickFolder)

	pickFolder = func() (string, error) { return "/Users/me/app", nil }
	resp, body := s.req(t, "POST", "/api/pick-folder", map[string]any{}, c)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"path":"/Users/me/app"`) {
		t.Fatalf("pick: %d %s", resp.StatusCode, body)
	}

	pickFolder = func() (string, error) { return "", errCanceled }
	resp, body = s.req(t, "POST", "/api/pick-folder", map[string]any{}, c)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"path":""`) {
		t.Fatalf("cancel should be 200 with empty path: %d %s", resp.StatusCode, body)
	}

	pickFolder = func() (string, error) { return "", errors.New("boom") }
	if resp, _ = s.req(t, "POST", "/api/pick-folder", map[string]any{}, c); resp.StatusCode != 500 {
		t.Fatalf("failure should be 500, got %d", resp.StatusCode)
	}

	if resp, _ = s.req(t, "POST", "/api/pick-folder", map[string]any{}, nil); resp.StatusCode != 401 {
		t.Fatalf("needs auth, got %d", resp.StatusCode)
	}
}
