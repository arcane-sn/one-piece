package api

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// pathID parses the {id} path value; a non-numeric id is simply "not found".
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, store.ErrNotFound
	}
	return id, nil
}

type boardIn struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Folders     []string `json:"folders"`
	Key         string   `json:"key"` // optional; changes the prefix of new ticket identifiers
}

func (s *server) listBoards(w http.ResponseWriter, r *http.Request) {
	bs, err := s.st.ListBoards()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bs)
}

func (s *server) createBoard(w http.ResponseWriter, r *http.Request) {
	var in boardIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.st.CreateBoard(in.Name, in.Description, in.Folders)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *server) getBoard(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.st.GetBoard(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *server) updateBoard(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in boardIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.st.UpdateBoard(id, in.Name, in.Description, in.Folders)
	if err == nil && in.Key != "" {
		b, err = s.st.SetBoardKey(id, strings.ToUpper(strings.TrimSpace(in.Key)))
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *server) deleteBoard(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.stopTurn(id)
	if err := s.st.DeleteBoard(id); err != nil {
		writeErr(w, err)
		return
	}
	os.Remove(s.st.PlanPath(id)) // the plan document goes with its board
	w.WriteHeader(http.StatusNoContent)
}
