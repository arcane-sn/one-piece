package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// approveContract makes the card approvable (a live Agent Task and QA case; any missing kind is added already
// done so it never runs) and approves it through the API. The caller records the passing review first.
func (s *testSrv) approveContract(t *testing.T, cp string, ck *http.Cookie, cardID int64) *http.Response {
	t.Helper()
	items, _ := s.St.ListItems(cardID)
	have := map[string]bool{}
	for _, it := range items {
		if !it.Superseded {
			have[it.Kind] = true
		}
	}
	for _, k := range []string{"agent", "qa"} {
		if !have[k] {
			it, err := s.St.CreateItem(cardID, k, "placeholder "+k, "")
			if err != nil {
				t.Fatal(err)
			}
			s.St.SetItemStatus(it.ID, "done")
		}
	}
	resp, _ := s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck)
	return resp
}

// contractFixture: a Business card with one Agent Task and one QA case, reviewed and approved.
func contractFixture(t *testing.T) (s *testSrv, ck *http.Cookie, cp string, card store.Card, agent, qa store.Item) {
	t.Helper()
	useStub(t, passing, nil)
	s = newTestServer(t)
	ck = s.login(t)
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Sign in"}, ck)
	json.Unmarshal(body, &card)
	cp = "/api/cards/" + strconv.FormatInt(card.ID, 10)
	agent, _ = s.St.CreateItem(card.ID, "agent", "Add endpoint", "POST /api/login")
	qa, _ = s.St.CreateItem(card.ID, "qa", "Wrong password", "shows an error")
	reviewWaiter(t, s, cp, ck)("done")
	if resp := s.approveContract(t, cp, ck, card.ID); resp.StatusCode != 200 {
		t.Fatalf("approve: %d", resp.StatusCode)
	}
	return
}

// reapprove records a fresh passing review of the current version and approves again (adding items revokes approval).
func (s *testSrv) reapprove(t *testing.T, cp string, ck *http.Cookie, cardID int64) {
	t.Helper()
	reviewWaiter(t, s, cp, ck)("done")
	if resp := s.approveContract(t, cp, ck, cardID); resp.StatusCode != 200 {
		t.Fatalf("reapprove: %d", resp.StatusCode)
	}
}

func ip(it store.Item) string { return "/api/items/" + strconv.FormatInt(it.ID, 10) }

func TestContractIsLockedInAgent(t *testing.T) {
	s, ck, cp, card, agent, qa := contractFixture(t)
	if resp, body := s.req(t, "POST", cp+"/move", map[string]any{"status": "agent", "position": 0}, ck); resp.StatusCode != 200 {
		t.Fatalf("move to agent: %d %s", resp.StatusCode, body)
	}
	for name, call := range map[string]func() int{
		"edit": func() int {
			r, _ := s.req(t, "PUT", ip(agent), map[string]any{"title": "Changed", "body": "x"}, ck)
			return r.StatusCode
		},
		"add": func() int {
			r, _ := s.req(t, "POST", cp+"/items", map[string]any{"kind": "qa", "title": "New"}, ck)
			return r.StatusCode
		},
		"delete":  func() int { r, _ := s.req(t, "DELETE", ip(qa), nil, ck); return r.StatusCode },
		"restore": func() int { r, _ := s.req(t, "POST", ip(qa)+"/restore", map[string]any{}, ck); return r.StatusCode },
		"plan": func() int {
			r, _ := s.req(t, "POST", cp+"/branches", map[string]any{"repo": "/r", "baseBranch": "main"}, ck)
			return r.StatusCode
		},
		"unplan": func() int { r, _ := s.req(t, "DELETE", cp+"/branches?repo=/r", nil, ck); return r.StatusCode },
		"rebase": func() int {
			r, _ := s.req(t, "PUT", cp+"/branches", map[string]any{"repo": "/r", "baseBranch": "dev"}, ck)
			return r.StatusCode
		},
	} {
		if got := call(); got != 409 {
			t.Errorf("%s in Agent: %d, want 409", name, got)
		}
	}
	if got, _ := s.St.GetItem(agent.ID); got.Title != "Add endpoint" {
		t.Errorf("title changed to %q", got.Title)
	}
	// a status change is run-time bookkeeping, not an edit of the contract
	if resp, _ := s.req(t, "PUT", ip(agent), map[string]any{"title": "Add endpoint", "body": "POST /api/login", "status": "done"}, ck); resp.StatusCode != 200 {
		t.Errorf("status change in Agent: %d, want 200", resp.StatusCode)
	}
	if c, _ := s.St.GetCard(card.ID); !c.Approved {
		t.Error("run-time writes must not revoke approval")
	}
}

func TestBusinessEditRevokesApproval(t *testing.T) {
	for name, edit := range map[string]func(s *testSrv, ck *http.Cookie, cp string, agent, qa store.Item){
		"edit": func(s *testSrv, ck *http.Cookie, cp string, a, q store.Item) {
			s.req(t0, "PUT", ip(q), map[string]any{"title": "Changed"}, ck)
		},
		"add": func(s *testSrv, ck *http.Cookie, cp string, a, q store.Item) {
			s.req(t0, "POST", cp+"/items", map[string]any{"kind": "agent", "title": "More"}, ck)
		},
		"delete": func(s *testSrv, ck *http.Cookie, cp string, a, q store.Item) { s.req(t0, "DELETE", ip(a), nil, ck) },
	} {
		t.Run(name, func(t *testing.T) {
			t0 = t
			s, ck, cp, card, a, q := contractFixture(t)
			before, _ := s.St.GetCard(card.ID)
			edit(s, ck, cp, a, q)
			after, _ := s.St.GetCard(card.ID)
			if after.Approved || after.ContentVersion != before.ContentVersion+1 {
				t.Fatalf("after %s: approved=%v version %d -> %d", name, after.Approved, before.ContentVersion, after.ContentVersion)
			}
			if resp, _ := s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck); resp.StatusCode != 409 {
				t.Fatalf("the old review must not approve the new contract: %d", resp.StatusCode)
			}
		})
	}
}

var t0 *testing.T // the subtest's T for the edit closures above

func TestRunTimeWritesDoNotRevokeApproval(t *testing.T) {
	s, _, _, card, agent, _ := contractFixture(t)
	s.St.SetItemStatus(agent.ID, "doing")
	s.St.AppendItemLog(agent.ID, "Editing main.go")
	s.St.SaveItem(agent.ID, "Add endpoint", "POST /api/login", "done", []string{"Status: done"})
	if c, _ := s.St.GetCard(card.ID); !c.Approved {
		t.Fatal("run-time writes revoked approval")
	}
}

func TestBackToBusinessRefusedWhileARunIsGoing(t *testing.T) {
	s, ck, cp, card, agent, _ := contractFixture(t)
	s.St.UpdateBoard(card.BoardID, "B", "", []string{t.TempDir()})
	s.req(t, "POST", cp+"/move", map[string]any{"status": "agent", "position": 0}, ck)
	s.St.SetItemStatus(agent.ID, "todo")
	started := make(chan struct{})
	old := agentRun
	defer func() { agentRun = old }()
	agentRun = func(ctx context.Context, _ string, _ []string, _, _ string, _ func(string)) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	if resp, body := s.req(t, "POST", cp+"/run-all", map[string]any{}, ck); resp.StatusCode != 202 {
		t.Fatalf("run-all: %d %s", resp.StatusCode, body)
	}
	<-started
	if resp, _ := s.req(t, "POST", cp+"/move", map[string]any{"status": "business", "position": 0}, ck); resp.StatusCode != 409 {
		t.Fatalf("move back while running: %d, want 409", resp.StatusCode)
	}
	s.req(t, "POST", cp+"/stop-all", map[string]any{}, ck)
}
