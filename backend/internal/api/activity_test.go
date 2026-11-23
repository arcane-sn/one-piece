package api

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func TestActivityListsRunningGenerateJobs(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	_, body := s.req(t, "POST", bp+"/cards", map[string]any{"title": "Login"}, ck)
	var card store.Card
	json.Unmarshal(body, &card)
	if _, err := s.St.CreateJob(store.Job{Kind: "agent_tasks", Scope: "card", TargetID: card.ID, CardID: card.ID, Label: "Agent Tasks " + card.Ident}); err != nil {
		t.Fatal(err)
	}
	_, out := s.req(t, "GET", "/api/activity", nil, ck)
	var rows []activityRow
	json.Unmarshal(out, &rows)
	found := false
	for _, r := range rows {
		found = found || (r.Kind == "agent_tasks" && r.CardID == card.ID && r.Ident == card.Ident)
	}
	if !found {
		t.Fatalf("activity for card %s = %s", strconv.FormatInt(card.ID, 10), out)
	}
}
