package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// commitInfo reads the commit the task just made in repo: its SHA and the files it touched (with line counts).
func commitInfo(repo string) (string, []store.FileChange, error) {
	sha, err := repoGit(repo, "rev-parse", "HEAD")
	if err != nil {
		return "", nil, err
	}
	out, err := repoGit(repo, "show", "--numstat", "--format=", "--no-renames", "HEAD")
	if err != nil {
		return sha, nil, err
	}
	var files []store.FileChange
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		f := store.FileChange{Path: parts[2]}
		if parts[0] == "-" { // binary
			f.Additions = -1
		} else {
			f.Additions, _ = strconv.Atoi(parts[0])
			f.Deletions, _ = strconv.Atoi(parts[1])
		}
		files = append(files, f)
		if len(files) >= 300 {
			break
		}
	}
	return sha, files, nil
}

// GET /api/items/{id}/changes lists what the Agent Task committed, per repo.
func (s *server) listItemChanges(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err == nil {
		_, err = s.st.GetItem(id)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	l, err := s.st.ListItemChanges(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

const maxDiffBytes = 200 << 10

// GET /api/item-changes/{id}/diff?file=path returns one file's unified diff of that commit (read-only; the file
// must be one the commit touched, and the diff is capped).
func (s *server) getChangeDiff(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	var c store.ItemChange
	if err == nil {
		c, err = s.st.GetItemChange(id)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	file := r.URL.Query().Get("file")
	known := false
	for _, f := range c.Files {
		known = known || f.Path == file
	}
	if !known {
		writeErr(w, store.ErrNotFound)
		return
	}
	out, err := repoGit(c.Repo, "show", "--no-color", "--no-renames", "-U3", "--format=", c.SHA, "--", file)
	if err != nil {
		writeErr(w, store.ConflictError{Msg: "The commit is no longer in the repo (rebased or deleted?)."})
		return
	}
	truncated := false
	if len(out) > maxDiffBytes {
		out, truncated = out[:maxDiffBytes], true
	}
	writeJSON(w, http.StatusOK, map[string]any{"diff": out, "truncated": truncated})
}
