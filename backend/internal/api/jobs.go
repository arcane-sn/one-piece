package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// jobWork is the body of a background Generate. It returns the JSON-able result, or an error whose
// text the owner will read. consume reports that the result was already applied (Agent Tasks, QA).
type jobWork func(ctx context.Context) (result any, consume bool, err error)

// startJob records a running job, answers 202 with it and runs work on a context the request does not
// own, so closing the modal (or the tab) does not stop it.
func (s *server) startJob(w http.ResponseWriter, j store.Job, timeout time.Duration, work jobWork) {
	j, err := s.st.CreateJob(j)
	if err != nil {
		writeErr(w, err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		res, consume, err := work(ctx)
		msg, out := "", ""
		if err != nil {
			log.Printf("job %d (%s) failed: %v", j.ID, j.Kind, err)
			msg = err.Error()
		} else if b, jerr := json.Marshal(res); jerr == nil {
			out = string(b)
		}
		if ferr := s.st.FinishJob(j.ID, out, msg, consume); ferr != nil {
			log.Printf("job %d: recording the outcome: %v", j.ID, ferr)
		}
	}()
	writeJSON(w, http.StatusAccepted, j)
}

// GET /api/jobs?scope=card&target=12 → the newest job of each kind for that target.
func (s *server) listJobs(w http.ResponseWriter, r *http.Request) {
	target, err := strconv.ParseInt(r.URL.Query().Get("target"), 10, 64)
	scope := r.URL.Query().Get("scope")
	if err != nil || (scope != "card" && scope != "item" && scope != "board") {
		writeErr(w, store.ValidationError{Msg: "scope (card|item|board) and target are required"})
		return
	}
	js, err := s.st.LatestJobs(scope, target)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, js)
}

// GET /api/jobs/active → running jobs and finished ones whose toast was not shown yet.
func (s *server) activeJobs(w http.ResponseWriter, r *http.Request) {
	js, err := s.st.ActiveJobs()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, js)
}

func (s *server) markJob(field string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err == nil {
			err = s.st.MarkJob(id, field)
		}
		if err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /api/items/{id}/restore brings a struck-through item back.
func (s *server) restoreItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	it, err := s.st.SetSuperseded(id, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.st.AppendItemLog(id, "Restored by you")
	it, _ = s.st.GetItem(it.ID)
	writeJSON(w, http.StatusOK, it)
}
