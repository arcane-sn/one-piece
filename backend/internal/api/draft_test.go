package api

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func TestDraftGates(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)

	// mk creates a card, waits for its on-save review to settle, and parks it in Draft.
	mk := func() string {
		_, body := s.req(t, "POST", bp+"/cards", map[string]string{"title": "T", "description": "d"}, ck)
		var card store.Card
		json.Unmarshal(body, &card)
		cp := "/api/cards/" + strconv.FormatInt(card.ID, 10)
		reviewWaiter(t, s, cp, ck)("done")
		if resp, b := s.req(t, "POST", cp+"/move", map[string]any{"status": "draft", "position": 0}, ck); resp.StatusCode != 200 {
			t.Fatalf("move to draft: %d %s", resp.StatusCode, b)
		}
		return cp
	}

	t.Run("ReviewDraftCardIs409", func(t *testing.T) {
		cp := mk()
		if resp, b := s.req(t, "POST", cp+"/review", nil, ck); resp.StatusCode != 409 {
			t.Fatalf("review of a draft: %d %s", resp.StatusCode, b)
		}
	})

	t.Run("UpdateDraftCardStartsNoReview", func(t *testing.T) {
		cp := mk()
		var before, after store.Review
		_, b := s.req(t, "GET", cp+"/review", nil, ck)
		json.Unmarshal(b, &before)
		if resp, b := s.req(t, "PUT", cp, map[string]string{"title": "T changed", "description": "d2"}, ck); resp.StatusCode != 200 {
			t.Fatalf("update draft: %d %s", resp.StatusCode, b)
		}
		_, b = s.req(t, "GET", cp+"/review", nil, ck)
		json.Unmarshal(b, &after)
		if after.ID != before.ID {
			t.Fatalf("saving a draft started a review: before %d, after %d", before.ID, after.ID)
		}
	})

	t.Run("MoveDraftToAgentIs409", func(t *testing.T) {
		cp := mk()
		if resp, b := s.req(t, "POST", cp+"/move", map[string]any{"status": "agent", "position": 0}, ck); resp.StatusCode != 409 {
			t.Fatalf("draft -> agent: %d %s", resp.StatusCode, b)
		}
	})
}
