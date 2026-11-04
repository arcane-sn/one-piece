package api

import (
	"log"
	"net/http"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

type cardIn struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Deadline    string `json:"deadline"`
}

func (s *server) listCards(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	cs, err := s.st.ListCards(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (s *server) createCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in cardIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.st.CreateCard(id, in.Title, in.Description, in.Deadline)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.reviewQuietly(c)
	writeJSON(w, http.StatusCreated, c)
}

func (s *server) updateCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in cardIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.st.UpdateCard(id, in.Title, in.Description, in.Deadline)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.reviewQuietly(c)
	writeJSON(w, http.StatusOK, c)
}

func (s *server) moveCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Status   string `json:"status"`
		Position int    `json:"position"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.st.MoveCard(id, in.Status, in.Position)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *server) deleteCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.st.DeleteCard(id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reviewQuietly starts a review for the saved card; a failure to start must not fail the save.
func (s *server) reviewQuietly(c store.Card) {
	if c.Status == "draft" {
		return
	}
	if _, err := s.startReview(c); err != nil {
		log.Printf("starting review of card %d: %v", c.ID, err)
	}
}
