package api

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func TestDecisionRoutes(t *testing.T) {
	s, ck, cp, card, _ := dFixture(t)
	if resp, _ := s.req(t, "POST", cp+"/decisions", map[string]string{"text": "  "}, ck); resp.StatusCode != 400 {
		t.Fatalf("empty: %d", resp.StatusCode)
	}
	resp, body := s.req(t, "POST", cp+"/decisions", map[string]string{"text": "Use release/2.0"}, ck)
	var d store.Decision
	json.Unmarshal(body, &d)
	if resp.StatusCode != 201 || d.Source != "owner" || d.CreatedAt == "" {
		t.Fatalf("add: %d %s", resp.StatusCode, body)
	}
	s.St.AddDecision(card.ID, "Merge for api", "claude")
	_, body = s.req(t, "GET", cp+"/decisions", nil, ck)
	var l []store.Decision
	json.Unmarshal(body, &l)
	if len(l) != 2 || l[0].Text != "Use release/2.0" || l[1].Source != "claude" {
		t.Fatalf("list = %s", body)
	}
	if resp, _ := s.req(t, "DELETE", cp+"/decisions/"+strconv.FormatInt(d.ID, 10), nil, ck); resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp, _ := s.req(t, "DELETE", cp+"/decisions/9999", nil, ck); resp.StatusCode != 404 {
		t.Fatalf("delete missing: %d", resp.StatusCode)
	}
	if resp, _ := s.req(t, "GET", "/api/cards/9999/decisions", nil, ck); resp.StatusCode != 404 {
		t.Fatalf("unknown card: %d", resp.StatusCode)
	}
	if resp, _ := s.req(t, "GET", cp+"/decisions", nil, nil); resp.StatusCode != 401 {
		t.Fatalf("no session: %d", resp.StatusCode)
	}
	s.St.AddStash(card.ID, "/r", "abc", "feature/X", "X wip")
	_, body = s.req(t, "GET", cp+"/stashes", nil, ck)
	if !strings.Contains(string(body), `"message":"X wip"`) {
		t.Errorf("stashes = %s", body)
	}
}

func TestBranchModeViaPut(t *testing.T) {
	s, ck, cp, card, _ := dFixture(t)
	s.St.DB.Exec(`INSERT INTO card_branches(card_id,repo,base_branch,feature_branch,dev_branch) VALUES(?,?,?,?,?)`, card.ID, "/r", "release", "f", "f-dev")
	resp, body := s.req(t, "PUT", cp+"/branches", map[string]any{"repo": "/r", "mode": "cherry-pick"}, ck)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"mode":"cherry-pick"`) {
		t.Fatalf("put mode: %d %s", resp.StatusCode, body)
	}
	if resp, _ := s.req(t, "PUT", cp+"/branches", map[string]any{"repo": "/r", "mode": "rebase"}, ck); resp.StatusCode != 400 {
		t.Fatalf("bad mode: %d", resp.StatusCode)
	}
}

func approvedAgentItem(t *testing.T, s *testSrv, ck any, cp string, card store.Card) string {
	t.Helper()
	s.St.DB.Exec(`UPDATE cards SET approved=1 WHERE id=?`, card.ID)
	it, err := s.St.CreateItem(card.ID, "agent", "Add endpoint", "POST /api/login")
	if err != nil {
		t.Fatal(err)
	}
	return "/api/items/" + strconv.FormatInt(it.ID, 10)
}

func TestRunCarriesDecisionLogAndExcludesDiscuss(t *testing.T) {
	s, ck, cp, card, _ := dFixture(t)
	s.St.AddDecision(card.ID, "Release branch is release/2.0", "claude")
	ip := approvedAgentItem(t, s, ck, cp, card)

	started, release := make(chan struct{}), make(chan struct{})
	var gotUser string
	old := agentRun
	defer func() { agentRun = old }()
	agentRun = func(_ context.Context, _ string, _ []string, _, user string, _ func(string)) (string, error) {
		gotUser = user
		close(started)
		<-release
		return "ok", nil
	}
	if resp, body := s.req(t, "POST", ip+"/run", map[string]any{}, ck); resp.StatusCode != 202 {
		t.Fatalf("run: %d %s", resp.StatusCode, body)
	}
	<-started
	if !strings.Contains(gotUser, "Decision log") || !strings.Contains(gotUser, "release/2.0") {
		t.Errorf("run message lacks the decision log:\n%s", gotUser)
	}
	useDiscuss(t, func(context.Context, discussRun, func(string)) (string, error) { return "s", nil })
	if code, body := dSend(t, s, cp, ck, "hi"); code != 409 || !strings.Contains(body, "Run is working") {
		t.Errorf("discuss during a run: %d %s", code, body)
	}
	close(release)
	waitItem(t, s, cp, ip, ck, "done")

	// the other way round: Run is refused while a Discuss turn runs
	hold, entered := make(chan struct{}), make(chan struct{})
	useDiscuss(t, func(ctx context.Context, _ discussRun, _ func(string)) (string, error) {
		close(entered)
		select {
		case <-hold:
		case <-ctx.Done():
		}
		return "s", nil
	})
	it2, _ := s.St.CreateItem(card.ID, "agent", "Second", "body")
	ip2 := "/api/items/" + strconv.FormatInt(it2.ID, 10)
	if code, body := dSend(t, s, cp, ck, "go"); code != 202 {
		t.Fatalf("discuss: %d %s", code, body)
	}
	<-entered
	if resp, body := s.req(t, "POST", ip2+"/run", map[string]any{}, ck); resp.StatusCode != 409 || !strings.Contains(string(body), "Discuss") {
		t.Errorf("run during discuss: %d %s", resp.StatusCode, body)
	}
	close(hold)
	dIdle(t, s, cp, ck)
}
