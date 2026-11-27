package api

import (
	"log"
	"net/http"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// The contract of a ticket (description, Agent Tasks, QA Test Cases, branch plan) is settled and approved in
// Business. Once the ticket is in Agent or Done it is locked: the only way to change it is to move the ticket back
// to Business, which revokes the approval.

// editContract writes a 409 and returns false when the card's contract is locked.
func (s *server) editContract(w http.ResponseWriter, cardID int64) bool {
	c, err := s.st.GetCard(cardID)
	if err != nil {
		writeErr(w, err)
		return false
	}
	if c.Status == "agent" || c.Status == "done" {
		writeErr(w, store.ConflictError{Msg: "The ticket is approved and running. Move it back to Business to change the plan."})
		return false
	}
	return true
}

// contractChanged records that the contract was edited: new version, approval revoked.
func (s *server) contractChanged(cardID int64) {
	if err := s.st.TouchCard(cardID); err != nil {
		log.Printf("contract of card %d changed but could not be marked: %v", cardID, err)
	}
}

// cardRunning reports whether a Run All chain or a single task run is working on the card.
func (s *server) cardRunning(cardID int64) bool {
	s.mu.Lock()
	_, chain := s.chains[cardID]
	s.mu.Unlock()
	if chain {
		return true
	}
	items, _ := s.st.ListItems(cardID)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
		if _, ok := s.runs[it.ID]; ok {
			return true
		}
	}
	return false
}
