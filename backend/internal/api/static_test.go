package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSPAFallback(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX"), 0o644)
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("JS"), 0o644)
	s := newTestServerDir(t, dir)

	get := func(p string) (int, string) {
		resp, err := http.Get(s.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := get("/boards/1"); code != 200 || body != "INDEX" {
		t.Errorf("/boards/1 = %d %q", code, body)
	}
	if code, body := get("/assets/app.js"); code != 200 || body != "JS" {
		t.Errorf("/assets/app.js = %d %q", code, body)
	}
	if code, body := get("/../../etc/passwd"); code != 200 || body != "INDEX" {
		t.Errorf("traversal = %d %q", code, body)
	}

	resp, body := s.req(t, "GET", "/api/nope", nil, s.login(t))
	if resp.StatusCode != 404 || !strings.Contains(string(body), `"error"`) {
		t.Errorf("/api/nope = %d %s", resp.StatusCode, body)
	}
}
