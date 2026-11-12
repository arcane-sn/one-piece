package api

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFilePickerAndMentions(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	root := filepath.Join(t.TempDir(), "shop-api")
	writeTree(t, root, "src/help/send.go", "src/main.go", "README.md", "node_modules/x/index.js", ".git/config", ".env")
	bp := newBoardPath(t, s, ck)
	if resp, body := s.req(t, "PUT", bp, map[string]any{"name": "B", "folders": []string{root}}, ck); resp.StatusCode != 200 {
		t.Fatalf("folders: %d %s", resp.StatusCode, body)
	}
	list := func(q string) (out []fileHit) {
		_, body := s.req(t, "GET", bp+"/files?q="+q, nil, ck)
		json.Unmarshal(body, &out)
		return
	}
	var top []string
	for _, h := range list("") {
		top = append(top, h.Path)
	}
	if got := strings.Join(top, ","); !strings.Contains(got, "shop-api/src") || !strings.Contains(got, "shop-api/README.md") || strings.Contains(got, "node_modules") || strings.Contains(got, ".git") || strings.Contains(got, ".env") || strings.Contains(got, "send.go") {
		t.Fatalf("top level = %s", got)
	}
	hits := list("SEND")
	if len(hits) != 1 || hits[0].Path != "shop-api/src/help/send.go" || hits[0].IsDir {
		t.Fatalf("search = %+v", hits)
	}
	if len(list("index.js")) != 0 || len(list("config")) != 0 {
		t.Error("node_modules and dot folders must be skipped")
	}

	paths, roots := mentionedPaths([]string{root}, "see @shop-api/src/help/send.go, and @shop-api/../../etc/passwd and @shop-api/nope and @other/x")
	if len(paths) != 1 || paths[0] != filepath.Join(root, "src/help/send.go") || len(roots) != 1 || roots[0] != root {
		t.Fatalf("mentions = %v %v", paths, roots)
	}

	// a mention gives Claude read access to that folder; without one the tool-less path is used
	var gotDirs []string
	var gotUser string
	defer func(f func(context.Context, string, string, []string, func(string)) (string, error)) {
		streamClaude = f
	}(streamClaude)
	streamClaude = func(_ context.Context, _, user string, dirs []string, _ func(string)) (string, error) {
		gotUser, gotDirs = user, dirs
		return "## Summary\nok", nil
	}
	body := map[string]any{"prompt": "log the send action, code in @shop-api/src/help/send.go", "boardId": 1}
	resp, out := s.req(t, "POST", "/api/generate-description", body, ck)
	if resp.StatusCode != 200 || len(gotDirs) != 1 || gotDirs[0] != root || !strings.Contains(gotUser, filepath.Join(root, "src/help/send.go")) {
		t.Fatalf("mention generate: %d %s dirs=%v\n%s", resp.StatusCode, out, gotDirs, gotUser)
	}
}

func gitInitDir(t *testing.T, dir string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
}

func TestFilePickerMarksAndRanksRepos(t *testing.T) {
	s := newTestServer(t)
	ck := s.login(t)
	master := filepath.Join(t.TempDir(), "master")
	gitInitDir(t, filepath.Join(master, "repoA"))
	writeTree(t, master, "docs/a.md", "zfile.txt", "repoA/src/x.go")
	bp := newBoardPath(t, s, ck)
	s.req(t, "PUT", bp, map[string]any{"name": "B", "folders": []string{master}}, ck)
	list := func(q string) (out []fileHit) {
		_, body := s.req(t, "GET", bp+"/files?q="+q, nil, ck)
		json.Unmarshal(body, &out)
		return
	}
	top := list("")
	var ia, id = -1, -1
	for i, h := range top {
		switch h.Path {
		case "master/repoA":
			ia = i
			if !h.IsRepo {
				t.Error("repoA must be marked isRepo")
			}
		case "master/docs":
			id = i
			if h.IsRepo {
				t.Error("docs is not a repo")
			}
		}
	}
	if ia < 0 || id < 0 || ia > id {
		t.Fatalf("repoA must be listed and rank before docs: %+v", top)
	}
	found := false
	for _, h := range list("repoa") {
		found = found || (h.Path == "master/repoA" && h.IsRepo)
	}
	if !found {
		t.Error("q=repoa must find the repo")
	}
}

func TestRepoMentions(t *testing.T) {
	master, _ := filepath.EvalSymlinks(t.TempDir())
	master = filepath.Join(master, "master")
	gitInitDir(t, filepath.Join(master, "repoA"))
	writeTree(t, master, "docs/a.md", "repoA/src/x.go")
	repos, not := repoMentions([]string{master}, "see @master/repoA/src/x.go and @master/repoA/ again @master/docs and @master/../x and @other/y")
	if len(repos) != 1 || repos[0] != filepath.Join(master, "repoA") {
		t.Errorf("repos = %v", repos)
	}
	if len(not) != 1 || not[0] != filepath.Join(master, "docs") {
		t.Errorf("notRepos = %v", not)
	}
}
