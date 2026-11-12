package api

import (
	"net/http"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

type branchIn struct {
	Repo          string  `json:"repo"`
	BaseBranch    string  `json:"baseBranch"`
	FeatureBranch *string `json:"featureBranch"`
	DevBranch     *string `json:"devBranch"`
	Status        *string `json:"status"`
	PRURL         *string `json:"prUrl"`
	Note          *string `json:"note"`
}

func (s *server) branchCard(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := pathID(r)
	if err == nil {
		_, err = s.st.GetCard(id)
	}
	if err != nil {
		writeErr(w, err)
		return 0, false
	}
	return id, true
}

func (s *server) listBranches(w http.ResponseWriter, r *http.Request) {
	id, ok := s.branchCard(w, r)
	if !ok {
		return
	}
	l, err := s.st.ListBranches(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

// POST /api/cards/{id}/branches {repo, baseBranch}
func (s *server) planBranch(w http.ResponseWriter, r *http.Request) {
	id, ok := s.branchCard(w, r)
	if !ok {
		return
	}
	var in branchIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.st.PlanBranch(id, in.Repo, in.BaseBranch)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

// PUT /api/cards/{id}/branches {repo, ...fields to change}
func (s *server) updateBranch(w http.ResponseWriter, r *http.Request) {
	id, ok := s.branchCard(w, r)
	if !ok {
		return
	}
	var in branchIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	patch := store.BranchPatch{FeatureBranch: in.FeatureBranch, DevBranch: in.DevBranch, Status: in.Status, PRURL: in.PRURL, Note: in.Note}
	if in.BaseBranch != "" {
		patch.BaseBranch = &in.BaseBranch
	}
	b, err := s.st.UpdateBranch(id, in.Repo, patch)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// DELETE /api/cards/{id}/branches?repo=
func (s *server) deleteBranch(w http.ResponseWriter, r *http.Request) {
	id, ok := s.branchCard(w, r)
	if !ok {
		return
	}
	if err := s.st.DeleteBranch(id, r.URL.Query().Get("repo")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
