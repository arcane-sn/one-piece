package api

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

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

// liveItems counts the card's items of one kind that were not struck through.
func (s *server) liveItems(cardID int64, kind string) int {
	items, _ := s.st.ListItems(cardID)
	n := 0
	for _, it := range items {
		if it.Kind == kind && !it.Superseded {
			n++
		}
	}
	return n
}

// autoGenerateContract drafts the plan of a ticket that just moved from Draft to Business: Agent Tasks first
// (the QA prompt reads them), then QA Test Cases. A kind that already has live items is left alone, so a second
// visit to Business never replaces the owner's review.
func (s *server) autoGenerateContract(cardID int64) {
	for _, kind := range []string{"agent", "qa"} {
		s.autoGenerate(cardID, kind)
	}
}

// autoGenerate starts one generation through the same handler as the Generate button and waits for its job.
func (s *server) autoGenerate(cardID int64, kind string) {
	if s.liveItems(cardID, kind) > 0 {
		return
	}
	id := strconv.FormatInt(cardID, 10)
	r := httptest.NewRequest("POST", "/api/cards/"+id+"/generate-items", strings.NewReader(`{"kind":"`+kind+`"}`))
	r.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	s.generateItems(rec, r)
	if rec.Code != http.StatusAccepted {
		log.Printf("auto generate %s for card %d: %s", kind, cardID, strings.TrimSpace(rec.Body.String()))
		return
	}
	var job store.Job
	if json.Unmarshal(rec.Body.Bytes(), &job) != nil {
		return
	}
	for deadline := time.Now().Add(20 * time.Minute); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if j, err := s.st.GetJob(job.ID); err != nil || j.Status != "running" {
			return
		}
	}
}
