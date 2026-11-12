package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

type claudeStub struct {
	mu     sync.Mutex
	reply  string
	err    error
	user   string
	dirs   []string
	system string
}

func (c *claudeStub) set(reply string, err error) {
	c.mu.Lock()
	c.reply, c.err = reply, err
	c.mu.Unlock()
}

func (c *claudeStub) fn(_ context.Context, system, user string, dirs []string, onLog func(string)) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.system, c.user, c.dirs = system, user, dirs
	onLog("Reading main.go")
	return c.reply, c.err
}

const needsWork = `{"verdict":"needs_work","summary":"Too vague","missing":["No Definition of Done"]}`
const passing = `{"verdict":"pass","summary":"Clear and small","missing":[]}`

func useStub(t *testing.T, reply string, err error) *claudeStub {
	t.Helper()
	old := streamClaude
	stub := &claudeStub{}
	stub.set(reply, err)
	streamClaude = stub.fn
	t.Cleanup(func() { streamClaude = old })
	return stub
}

// reviewWaiter returns the card's passing review. Reviews no longer run by themselves: a test that
// needs an approved ticket records the verdict Claude would have recorded in the review chat.
func reviewWaiter(t *testing.T, s *testSrv, cardPath string, ck *http.Cookie) func(want string) store.Review {
	id, _ := strconv.ParseInt(cardPath[strings.LastIndex(cardPath, "/")+1:], 10, 64)
	return func(want string) store.Review {
		t.Helper()
		if _, err := s.St.RecordVerdict(id, "pass", "Clear and small", nil); err != nil {
			t.Fatal(err)
		}
		r, err := s.St.LatestReview(id)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
}

func TestNoReviewRunsOnSaveAndApprovalNeedsACurrentPass(t *testing.T) {
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Sign in"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)

	s.req(t, "PUT", cp, map[string]any{"title": "Login", "description": "Sign in. DoD: opens dashboard."}, ck)
	s.req(t, "POST", cp+"/flows", map[string]any{"title": "F", "source": "A -> B: x"}, ck)
	if _, body = s.req(t, "GET", cp+"/review", nil, ck); strings.TrimSpace(string(body)) != "null" {
		t.Fatalf("saving must not start a review: %s", body)
	}
	if resp, _ := s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck); resp.StatusCode != 409 {
		t.Fatalf("approve without a review: %d, want 409", resp.StatusCode)
	}
	if _, err := s.St.RecordVerdict(card.ID, "needs_work", "vague", []string{"no DoD"}); err != nil {
		t.Fatal(err)
	}
	if resp, _ := s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck); resp.StatusCode != 409 {
		t.Fatalf("approve on needs_work: %d, want 409", resp.StatusCode)
	}
	s.St.RecordVerdict(card.ID, "pass", "ok", nil)
	if resp, _ := s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck); resp.StatusCode != 200 {
		t.Fatalf("approve on pass: %d", resp.StatusCode)
	}
	_, body = s.req(t, "PUT", cp, map[string]any{"title": "Login v2", "description": "Sign in. DoD: opens dashboard."}, ck)
	var edited store.Card
	json.Unmarshal(body, &edited)
	if edited.Approved {
		t.Fatal("editing content must revoke approval")
	}
	if resp, _ := s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck); resp.StatusCode != 409 {
		t.Fatalf("a verdict of an older version must not approve: %d", resp.StatusCode)
	}
}
