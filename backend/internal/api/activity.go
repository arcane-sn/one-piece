package api

import (
	"net/http"
	"sort"
)

// activityRow is one thing Claude is doing right now: a chat turn, an Agent Task run or a Run All chain.
type activityRow struct {
	Kind    string `json:"kind"` // discuss | review | brainstorm | agent | chain
	BoardID int64  `json:"boardId"`
	Board   string `json:"board"`
	CardID  int64  `json:"cardId"` // 0 for brainstorm
	Ident   string `json:"ident"`
	Title   string `json:"title"`
}

// GET /api/activity lists everything running now (read from the in-memory run maps, so it is cheap to poll).
func (s *server) activity(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	var boards, cardsD, cardsR, items, chains []int64
	for id := range s.turns {
		boards = append(boards, id)
	}
	for id := range s.discuss {
		cardsD = append(cardsD, id)
	}
	for id := range s.rchats {
		cardsR = append(cardsR, id)
	}
	for id := range s.runs {
		items = append(items, id)
	}
	for id := range s.chains {
		chains = append(chains, id)
	}
	s.mu.Unlock()

	out := []activityRow{}
	boardName := map[int64]string{}
	name := func(id int64) string {
		if n, ok := boardName[id]; ok {
			return n
		}
		b, _ := s.st.GetBoard(id)
		boardName[id] = b.Name
		return b.Name
	}
	card := func(kind string, id int64) {
		if c, err := s.st.GetCard(id); err == nil {
			out = append(out, activityRow{Kind: kind, BoardID: c.BoardID, Board: name(c.BoardID), CardID: c.ID, Ident: c.Ident, Title: c.Title})
		}
	}
	for _, id := range boards {
		out = append(out, activityRow{Kind: "brainstorm", BoardID: id, Board: name(id), Title: "Brainstorming"})
	}
	for _, id := range cardsD {
		card("discuss", id)
	}
	for _, id := range cardsR {
		card("review", id)
	}
	for _, id := range chains {
		card("chain", id)
	}
	seen := map[int64]bool{}
	for _, id := range items {
		if it, err := s.st.GetItem(id); err == nil && !seen[it.CardID] {
			seen[it.CardID] = true
			card("agent", it.CardID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ident < out[j].Ident })
	writeJSON(w, http.StatusOK, out)
}
