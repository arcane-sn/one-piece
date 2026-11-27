package api

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestShipGuardsAndDirectPush(t *testing.T) {
	s, ck, cp, card, _ := dFixture(t)
	ship := cp + "/ship"

	if resp, body := s.req(t, "POST", ship, nil, ck); resp.StatusCode != 409 || !strings.Contains(string(body), "Done") {
		t.Fatalf("not done: %d %s", resp.StatusCode, body)
	}
	s.St.DB.Exec(`UPDATE cards SET status='done' WHERE id=?`, card.ID)
	if resp, body := s.req(t, "POST", ship, nil, ck); resp.StatusCode != 409 || !strings.Contains(string(body), "No repo") {
		t.Fatalf("no repo: %d %s", resp.StatusCode, body)
	}

	remote, repo := t.TempDir(), t.TempDir()
	gitIn(t, remote, "init", "--bare", "-b", "main")
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "config", "user.name", "t")
	gitIn(t, repo, "config", "user.email", "t@t")
	gitIn(t, repo, "remote", "add", "origin", remote)
	gitIn(t, repo, "commit", "--allow-empty", "-m", "base")
	gitIn(t, repo, "checkout", "-b", "feature/X")
	gitIn(t, repo, "commit", "--allow-empty", "-m", "work")
	s.St.DB.Exec(`INSERT INTO card_branches(card_id,repo,base_branch,feature_branch,dev_branch,status,via_dev) VALUES(?,?,?,?,?,?,0)`,
		card.ID, repo, "main", "feature/X", "feature/X-dev", "branched")

	// a via -dev repo that is not merged yet blocks the whole ship
	s.St.DB.Exec(`INSERT INTO card_branches(card_id,repo,base_branch,feature_branch,dev_branch,status,via_dev) VALUES(?,?,?,?,?,?,1)`,
		card.ID, "/other", "main", "f", "f-dev", "branched")
	if resp, body := s.req(t, "POST", ship, nil, ck); resp.StatusCode != 409 || !strings.Contains(string(body), "not merged") {
		t.Fatalf("via-dev unmerged: %d %s", resp.StatusCode, body)
	}
	s.St.DB.Exec(`DELETE FROM card_branches WHERE repo='/other'`)

	resp, body := s.req(t, "POST", ship, nil, ck)
	var res []shipResult
	json.Unmarshal(body, &res)
	if resp.StatusCode != 200 || len(res) != 1 || res[0].Status != "pushed" {
		t.Fatalf("ship: %d %s", resp.StatusCode, body)
	}
	out, _ := exec.Command("git", "-C", remote, "branch", "--list", "feature/X").Output()
	if !strings.Contains(string(out), "feature/X") {
		t.Fatalf("branch not pushed: %q", out)
	}
	if rows, _ := s.St.ListBranches(card.ID); rows[0].Status != "pushed" {
		t.Fatalf("status = %s", rows[0].Status)
	}
}
