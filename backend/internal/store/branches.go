package store

import (
	"database/sql"
	"errors"
	"regexp"

	"github.com/arcane-sn/one-piece/backend/internal/gitx"
)

var prefixRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/$`)

// BranchStatuses are the states of one repo's branching for a ticket.
var BranchStatuses = []string{"planned", "branched", "merged_dev", "pr_open", "blocked"}

// Branch is one repo's branching plan for a ticket (the owner's Discuss session decides it).
type Branch struct {
	ID            int64  `json:"id"`
	CardID        int64  `json:"cardId"`
	Repo          string `json:"repo"`
	BaseBranch    string `json:"baseBranch"`
	FeatureBranch string `json:"featureBranch"`
	DevBranch     string `json:"devBranch"`
	Status        string `json:"status"`
	PRURL         string `json:"prUrl"`
	Note          string `json:"note"`
}

// BranchPatch changes the given fields only.
type BranchPatch struct {
	BaseBranch, FeatureBranch, DevBranch, Status, PRURL, Note *string
}

const branchCols = `id,card_id,repo,base_branch,feature_branch,dev_branch,status,pr_url,note`

func scanBranch(sc interface{ Scan(...any) error }) (Branch, error) {
	var b Branch
	err := sc.Scan(&b.ID, &b.CardID, &b.Repo, &b.BaseBranch, &b.FeatureBranch, &b.DevBranch, &b.Status, &b.PRURL, &b.Note)
	if errors.Is(err, sql.ErrNoRows) {
		return Branch{}, ErrNotFound
	}
	return b, err
}

// SetBoardBranchDefaults changes how new branch plans are named; existing rows keep their names.
func (s *Store) SetBoardBranchDefaults(id int64, prefix, devBranch string) (Board, error) {
	if !prefixRe.MatchString(prefix) || !gitx.ValidBranch(prefix[:len(prefix)-1]) {
		return Board{}, ValidationError{"branch prefix must be lowercase and end with /, e.g. feature/"}
	}
	if !gitx.ValidBranch(devBranch) {
		return Board{}, ValidationError{"dev branch is not a valid branch name"}
	}
	res, err := s.DB.Exec(`UPDATE boards SET branch_prefix=?, dev_branch=? WHERE id=?`, prefix, devBranch, id)
	if err != nil {
		return Board{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Board{}, ErrNotFound
	}
	return s.GetBoard(id)
}

func (s *Store) ListBranches(cardID int64) ([]Branch, error) {
	rows, err := s.DB.Query(`SELECT `+branchCols+` FROM card_branches WHERE card_id=? ORDER BY id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Branch{}
	for rows.Next() {
		b, err := scanBranch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// PlanBranch records (or, while still "planned", updates) the branching of one repo for a card.
// repo may be any path inside the board's first folder; it is stored as the repository root.
func (s *Store) PlanBranch(cardID int64, repo, baseBranch string) (Branch, error) {
	c, err := getCard(s.DB, cardID)
	if err != nil {
		return Branch{}, err
	}
	b, err := s.GetBoard(c.BoardID)
	if err != nil {
		return Branch{}, err
	}
	if len(b.Folders) == 0 {
		return Branch{}, ValidationError{"this board has no folder"}
	}
	if !gitx.ValidBranch(baseBranch) {
		return Branch{}, ValidationError{"base branch is not a valid branch name"}
	}
	root, err := gitx.RepoRootWithin(b.Folders[0], repo)
	if err != nil {
		return Branch{}, ValidationError{err.Error()}
	}
	feature := b.BranchPrefix + c.Ident
	var status string
	err = s.DB.QueryRow(`SELECT status FROM card_branches WHERE card_id=? AND repo=?`, cardID, root).Scan(&status)
	switch {
	case err == nil && status != "planned":
		return Branch{}, ConflictError{"This repo is already being branched. Change it from the Branching list."}
	case err == nil:
		_, err = s.DB.Exec(`UPDATE card_branches SET base_branch=? WHERE card_id=? AND repo=?`, baseBranch, cardID, root)
	case errors.Is(err, sql.ErrNoRows):
		_, err = s.DB.Exec(`INSERT INTO card_branches(card_id,repo,base_branch,feature_branch,dev_branch) VALUES(?,?,?,?,?)`,
			cardID, root, baseBranch, feature, feature+"-dev")
	}
	if err != nil {
		return Branch{}, err
	}
	return scanBranch(s.DB.QueryRow(`SELECT `+branchCols+` FROM card_branches WHERE card_id=? AND repo=?`, cardID, root))
}

func (s *Store) UpdateBranch(cardID int64, repo string, f BranchPatch) (Branch, error) {
	cur, err := scanBranch(s.DB.QueryRow(`SELECT `+branchCols+` FROM card_branches WHERE card_id=? AND repo=?`, cardID, repo))
	if err != nil {
		return Branch{}, err
	}
	for _, p := range []struct {
		v    *string
		dst  *string
		name string
	}{{f.BaseBranch, &cur.BaseBranch, "base branch"}, {f.FeatureBranch, &cur.FeatureBranch, "feature branch"}, {f.DevBranch, &cur.DevBranch, "dev branch"}} {
		if p.v == nil {
			continue
		}
		if !gitx.ValidBranch(*p.v) {
			return Branch{}, ValidationError{p.name + " is not a valid branch name"}
		}
		*p.dst = *p.v
	}
	if f.Status != nil {
		ok := false
		for _, v := range BranchStatuses {
			ok = ok || v == *f.Status
		}
		if !ok {
			return Branch{}, ValidationError{"unknown branch status"}
		}
		cur.Status = *f.Status
	}
	if f.PRURL != nil {
		cur.PRURL = *f.PRURL
	}
	if f.Note != nil {
		cur.Note = *f.Note
	}
	if len(cur.PRURL) > 500 || len(cur.Note) > 2000 {
		return Branch{}, ValidationError{"pr url or note is too long"}
	}
	_, err = s.DB.Exec(`UPDATE card_branches SET base_branch=?,feature_branch=?,dev_branch=?,status=?,pr_url=?,note=? WHERE id=?`,
		cur.BaseBranch, cur.FeatureBranch, cur.DevBranch, cur.Status, cur.PRURL, cur.Note, cur.ID)
	return cur, err
}

func (s *Store) DeleteBranch(cardID int64, repo string) error {
	res, err := s.DB.Exec(`DELETE FROM card_branches WHERE card_id=? AND repo=?`, cardID, repo)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
