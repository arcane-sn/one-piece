package api

import (
	"net/http"
	"strconv"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// GET /api/cards/{id}/decisions
func (s *server) listDecisions(w http.ResponseWriter, r *http.Request) {
	id, ok := s.branchCard(w, r)
	if !ok {
		return
	}
	l, err := s.st.ListDecisions(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

// POST /api/cards/{id}/decisions {"text": "..."}: the owner adds a note.
func (s *server) addDecision(w http.ResponseWriter, r *http.Request) {
	id, ok := s.branchCard(w, r)
	if !ok {
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	d, err := s.st.AddDecision(id, in.Text, "owner")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// DELETE /api/cards/{id}/decisions/{did}
func (s *server) deleteDecision(w http.ResponseWriter, r *http.Request) {
	id, ok := s.branchCard(w, r)
	if !ok {
		return
	}
	did, err := strconv.ParseInt(r.PathValue("did"), 10, 64)
	if err != nil {
		writeErr(w, store.ErrNotFound)
		return
	}
	if err := s.st.DeleteDecision(id, did); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/cards/{id}/stashes
func (s *server) listStashes(w http.ResponseWriter, r *http.Request) {
	id, ok := s.branchCard(w, r)
	if !ok {
		return
	}
	l, err := s.st.ListStashes(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}
