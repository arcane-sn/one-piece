package api

import "net/http"

type flowIn struct {
	Title  string `json:"title"`
	Source string `json:"source"`
}

func (s *server) listFlows(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.st.GetCard(id); err != nil {
		writeErr(w, err)
		return
	}
	fs, err := s.st.ListFlows(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fs)
}

func (s *server) createFlow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in flowIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	f, err := s.st.CreateFlow(id, in.Title, in.Source)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *server) updateFlow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in flowIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	f, err := s.st.UpdateFlow(id, in.Title, in.Source)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *server) deleteFlow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.st.DeleteFlow(id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
