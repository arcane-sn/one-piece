package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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

// reviewWaiter polls GET /api/cards/{id}/review until it reaches the wanted status.
func reviewWaiter(t *testing.T, s *testSrv, cardPath string, ck *http.Cookie) func(want string) store.Review {
	return func(want string) store.Review {
		t.Helper()
		var r store.Review
		for i := 0; i < 150; i++ {
			_, b := s.req(t, "GET", cardPath+"/review", nil, ck)
			r = store.Review{}
			json.Unmarshal(b, &r)
			if r.Status == want {
				return r
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("review never reached %q, last = %+v", want, r)
		return r
	}
}

func TestReviewRunsOnSaveAndGatesApproval(t *testing.T) {
	stub := useStub(t, needsWork, nil)
	s := newTestServer(t)
	ck := s.login(t)
	dir := t.TempDir()
	_, body := s.req(t, "POST", "/api/boards", map[string]any{"name": "Shop", "description": "Sells", "folders": []string{dir}}, ck)
	var board store.Board
	json.Unmarshal(body, &board)
	bp := "/api/boards/" + strconv.FormatInt(board.ID, 10)

	_, body = s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login", "description": "Let the owner sign in"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	wait := reviewWaiter(t, s, cp, ck)

	r := wait("done")
	if r.Verdict != "needs_work" || r.Summary != "Too vague" || !reflect.DeepEqual(r.Missing, []string{"No Definition of Done"}) ||
		!reflect.DeepEqual(r.Log, []string{"Reading main.go"}) || r.ContentVersion != 1 {
		t.Fatalf("review = %+v", r)
	}
	stub.mu.Lock()
	for _, want := range []string{"Login", "Let the owner sign in", "Shop", "Sells", dir} {
		if !strings.Contains(stub.user, want) {
			t.Errorf("review prompt missing %q:\n%s", want, stub.user)
		}
	}
	if !reflect.DeepEqual(stub.dirs, []string{dir}) {
		t.Errorf("claude should get the board folders, got %v", stub.dirs)
	}
	stub.mu.Unlock()

	if resp, _ := s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck); resp.StatusCode != 409 {
		t.Fatalf("approve on needs_work: %d, want 409", resp.StatusCode)
	}

	// a deadline-only edit must not trigger another review
	stub.set(passing, nil)
	s.req(t, "PUT", cp, map[string]any{"title": "Login", "description": "Let the owner sign in", "deadline": "2026-12-31"}, ck)
	time.Sleep(100 * time.Millisecond)
	if again := wait("done"); again.ID != r.ID {
		t.Fatalf("deadline-only edit started a new review: %+v", again)
	}

	// a content edit starts a new review that can pass
	s.req(t, "PUT", cp, map[string]any{"title": "Login", "description": "Let the owner sign in. DoD: valid login opens dashboard."}, ck)
	for i := 0; i < 150; i++ {
		r = wait("done")
		if r.ContentVersion == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if r.ContentVersion != 2 || r.Verdict != "pass" {
		t.Fatalf("second review = %+v", r)
	}
	resp, body := s.req(t, "POST", cp+"/approve", map[string]bool{"approved": true}, ck)
	var approved store.Card
	json.Unmarshal(body, &approved)
	if resp.StatusCode != 200 || !approved.Approved {
		t.Fatalf("approve: %d %s", resp.StatusCode, body)
	}
	_, body = s.req(t, "PUT", cp, map[string]any{"title": "Login v2", "description": "Let the owner sign in. DoD: valid login opens dashboard."}, ck)
	var edited store.Card
	json.Unmarshal(body, &edited)
	if edited.Approved {
		t.Fatal("editing content must revoke approval")
	}
}

func TestReviewFailureCanBeRetried(t *testing.T) {
	useStub(t, "", errors.New("claude exploded"))
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]string{"title": "T"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
	wait := reviewWaiter(t, s, cp, ck)

	if r := wait("failed"); r.Error == "" {
		t.Fatalf("failed review should explain itself: %+v", r)
	}
	useStub(t, passing, nil)
	if resp, _ := s.req(t, "POST", cp+"/review", map[string]any{}, ck); resp.StatusCode != 202 {
		t.Fatalf("retry: %d, want 202", resp.StatusCode)
	}
	if r := wait("done"); r.Verdict != "pass" {
		t.Fatalf("retried review = %+v", r)
	}
}

func TestParseVerdict(t *testing.T) {
	good := []struct{ in, verdict string }{
		{`{"verdict":"pass","summary":"ok","missing":[]}`, "pass"},
		{"```json\n{\"verdict\":\"needs_work\",\"summary\":\"s\",\"missing\":[\"a\"]}\n```", "needs_work"},
		{"Here is my review:\n{\"verdict\":\"pass\",\"summary\":\"fine\"}\nThanks", "pass"},
	}
	for _, g := range good {
		v, err := parseVerdict(g.in)
		if err != nil || v.Verdict != g.verdict {
			t.Errorf("parseVerdict(%q) = %+v, %v", g.in, v, err)
		}
	}
	for _, bad := range []string{"", "no json here", `{"verdict":"maybe","summary":"x"}`, `{"summary":"x"}`, `{not json}`} {
		if _, err := parseVerdict(bad); err == nil {
			t.Errorf("parseVerdict(%q) should fail", bad)
		}
	}
}
