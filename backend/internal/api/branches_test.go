package api

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func TestBranchesAPI(t *testing.T) {
	s := newTestServer(t)
	ck := s.login(t)
	master, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(master, "svc")
	os.MkdirAll(repo, 0o755)
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "PUT", bp, map[string]any{"name": "B", "folders": []string{master}, "branchPrefix": "work/", "devBranch": "integ"}, ck)
	var b store.Board
	json.Unmarshal(body, &b)
	if b.BranchPrefix != "work/" || b.DevBranch != "integ" {
		t.Fatalf("board defaults = %s", body)
	}
	if resp, _ := s.req(t, "PUT", bp, map[string]any{"name": "B", "folders": []string{master}, "branchPrefix": "Bad"}, ck); resp.StatusCode != 400 {
		t.Fatalf("bad prefix: %d", resp.StatusCode)
	}
	_, body = s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10) + "/branches"

	resp, body := s.req(t, "POST", cp, map[string]any{"repo": repo, "baseBranch": "main"}, ck)
	var br store.Branch
	json.Unmarshal(body, &br)
	if resp.StatusCode != 201 || br.FeatureBranch != "work/"+card.Ident || br.DevBranch != "work/"+card.Ident+"-dev" {
		t.Fatalf("post: %d %s", resp.StatusCode, body)
	}
	_, body = s.req(t, "GET", cp, nil, ck)
	var list []store.Branch
	json.Unmarshal(body, &list)
	if len(list) != 1 || list[0].Repo != repo {
		t.Fatalf("get = %s", body)
	}
	resp, body = s.req(t, "PUT", cp, map[string]any{"repo": repo, "status": "pr_open", "prUrl": "https://x/pr/1"}, ck)
	json.Unmarshal(body, &br)
	if resp.StatusCode != 200 || br.Status != "pr_open" || br.PRURL != "https://x/pr/1" {
		t.Fatalf("put: %d %s", resp.StatusCode, body)
	}
	if resp, _ := s.req(t, "PUT", cp, map[string]any{"repo": repo, "status": "weird"}, ck); resp.StatusCode != 400 {
		t.Fatalf("weird status: %d", resp.StatusCode)
	}
	if resp, _ := s.req(t, "POST", cp, map[string]any{"repo": t.TempDir(), "baseBranch": "main"}, ck); resp.StatusCode != 400 {
		t.Fatalf("outside repo: %d, want 400", resp.StatusCode)
	}
	if resp, _ := s.req(t, "DELETE", cp+"?repo="+url.QueryEscape(repo), nil, ck); resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp, _ := s.req(t, "DELETE", cp+"?repo="+url.QueryEscape(repo), nil, ck); resp.StatusCode != 404 {
		t.Fatalf("second delete: %d", resp.StatusCode)
	}
}
