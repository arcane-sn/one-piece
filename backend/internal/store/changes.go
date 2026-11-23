package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// FileChange is one file of a task's commit.
type FileChange struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"` // -1 for a binary file
	Deletions int    `json:"deletions"`
}

// ItemChange is what one Agent Task committed in one repo: the proof of what it changed.
type ItemChange struct {
	ID        int64        `json:"id"`
	ItemID    int64        `json:"itemId"`
	Repo      string       `json:"repo"`
	SHA       string       `json:"sha"`
	Files     []FileChange `json:"files"`
	Additions int          `json:"additions"`
	Deletions int          `json:"deletions"`
	CreatedAt string       `json:"createdAt"`
}

// ChangeSummary is the one-line version shown on a task row.
type ChangeSummary struct {
	Files     int `json:"files"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

func (s *Store) AddItemChange(itemID int64, repo, sha string, files []FileChange) error {
	add, del := 0, 0
	for _, f := range files {
		if f.Additions > 0 {
			add += f.Additions
		}
		del += f.Deletions
	}
	if files == nil {
		files = []FileChange{}
	}
	b, _ := json.Marshal(files)
	_, err := s.DB.Exec(`INSERT INTO item_changes(item_id,repo,sha,files,additions,deletions) VALUES(?,?,?,?,?,?)`, itemID, repo, sha, string(b), add, del)
	return err
}

// ClearItemChanges forgets a task's earlier commits when it runs again (the new run records its own).
func (s *Store) ClearItemChanges(itemID int64) error {
	_, err := s.DB.Exec(`DELETE FROM item_changes WHERE item_id=?`, itemID)
	return err
}

func scanChange(sc interface{ Scan(...any) error }) (ItemChange, error) {
	var c ItemChange
	var files string
	if err := sc.Scan(&c.ID, &c.ItemID, &c.Repo, &c.SHA, &files, &c.Additions, &c.Deletions, &c.CreatedAt); err != nil {
		return ItemChange{}, err
	}
	c.Files = []FileChange{}
	json.Unmarshal([]byte(files), &c.Files)
	return c, nil
}

const changeCols = `id,item_id,repo,sha,files,additions,deletions,created_at`

func (s *Store) ListItemChanges(itemID int64) ([]ItemChange, error) {
	rows, err := s.DB.Query(`SELECT `+changeCols+` FROM item_changes WHERE item_id=? ORDER BY id`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ItemChange{}
	for rows.Next() {
		c, err := scanChange(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetItemChange(id int64) (ItemChange, error) {
	c, err := scanChange(s.DB.QueryRow(`SELECT `+changeCols+` FROM item_changes WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ItemChange{}, ErrNotFound
	}
	return c, err
}

// attachChanges fills Item.Changes for the Agent Tasks of one card.
func (s *Store) attachChanges(cardID int64, items []Item) error {
	rows, err := s.DB.Query(`SELECT c.item_id, COUNT(*), SUM(c.additions), SUM(c.deletions), GROUP_CONCAT(c.files, char(31))
		FROM item_changes c JOIN card_items i ON i.id=c.item_id WHERE i.card_id=? GROUP BY c.item_id`, cardID)
	if err != nil {
		return err
	}
	defer rows.Close()
	sum := map[int64]*ChangeSummary{}
	for rows.Next() {
		var id int64
		var n, add, del int
		var blobs sql.NullString
		if err := rows.Scan(&id, &n, &add, &del, &blobs); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, blob := range splitUnit(blobs.String) {
			var fs []FileChange
			json.Unmarshal([]byte(blob), &fs)
			for _, f := range fs {
				seen[f.Path] = true
			}
		}
		sum[id] = &ChangeSummary{Files: len(seen), Additions: add, Deletions: del}
	}
	for i := range items {
		items[i].Changes = sum[items[i].ID]
	}
	return rows.Err()
}

func splitUnit(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == 31 {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
