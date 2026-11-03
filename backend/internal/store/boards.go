package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Board struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Folders     []string `json:"folders"`
	// Counts is the number of cards per status (all four keys always present).
	Counts map[string]int `json:"counts"`
}

func zeroCounts() map[string]int {
	m := make(map[string]int, len(Statuses))
	for _, s := range Statuses {
		m[s] = 0
	}
	return m
}

// cardCounts returns per-status card counts keyed by board id; boardID 0 means all boards.
func (s *Store) cardCounts(boardID int64) (map[int64]map[string]int, error) {
	rows, err := s.DB.Query(`SELECT board_id,status,COUNT(*) FROM cards WHERE ?=0 OR board_id=? GROUP BY board_id,status`, boardID, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[string]int{}
	for rows.Next() {
		var id int64
		var st string
		var n int
		if err := rows.Scan(&id, &st, &n); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = zeroCounts()
		}
		out[id][st] = n
	}
	return out, rows.Err()
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return "", ValidationError{"name must be 1-100 characters"}
	}
	return name, nil
}

func cleanDescription(d string) (string, error) {
	d = strings.TrimSpace(d)
	if utf8.RuneCountInString(d) > 500 {
		return "", ValidationError{"description must be at most 500 characters"}
	}
	return d, nil
}

// cleanFolders trims, requires absolute existing directories, normalizes with
// filepath.Clean and drops duplicates (keeping first-seen order). Blank entries are skipped.
func cleanFolders(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, f := range in {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !filepath.IsAbs(f) {
			return nil, ValidationError{"folder must be an absolute path: " + f}
		}
		f = filepath.Clean(f)
		if fi, err := os.Stat(f); err != nil || !fi.IsDir() {
			return nil, ValidationError{"folder does not exist or is not a directory: " + f}
		}
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out, nil
}

func saveFolders(tx *sql.Tx, boardID int64, folders []string) error {
	for i, f := range folders {
		if _, err := tx.Exec(`INSERT INTO board_folders(board_id,path,position) VALUES(?,?,?)`, boardID, f, i); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateBoard(name, description string, folders []string) (Board, error) {
	name, err := cleanName(name)
	if err != nil {
		return Board{}, err
	}
	description, err = cleanDescription(description)
	if err != nil {
		return Board{}, err
	}
	folders, err = cleanFolders(folders)
	if err != nil {
		return Board{}, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Board{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO boards(name,description) VALUES(?,?)`, name, description)
	if err != nil {
		return Board{}, err
	}
	id, _ := res.LastInsertId()
	if err := saveFolders(tx, id, folders); err != nil {
		return Board{}, err
	}
	if err := tx.Commit(); err != nil {
		return Board{}, err
	}
	return Board{ID: id, Name: name, Description: description, Folders: folders, Counts: zeroCounts()}, nil
}

func (s *Store) ListBoards() ([]Board, error) {
	boards := []Board{}
	idx := map[int64]int{}
	rows, err := s.DB.Query(`SELECT id,name,description FROM boards ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		b := Board{Folders: []string{}, Counts: zeroCounts()}
		if err := rows.Scan(&b.ID, &b.Name, &b.Description); err != nil {
			rows.Close()
			return nil, err
		}
		idx[b.ID] = len(boards)
		boards = append(boards, b)
	}
	rows.Close() // single connection: must finish before the next query
	frows, err := s.DB.Query(`SELECT board_id,path FROM board_folders ORDER BY board_id,position`)
	if err != nil {
		return nil, err
	}
	defer frows.Close()
	for frows.Next() {
		var id int64
		var p string
		if err := frows.Scan(&id, &p); err != nil {
			return nil, err
		}
		if i, ok := idx[id]; ok {
			boards[i].Folders = append(boards[i].Folders, p)
		}
	}
	if err := frows.Err(); err != nil {
		return nil, err
	}
	frows.Close() // single connection: release before the counts query
	counts, err := s.cardCounts(0)
	if err != nil {
		return nil, err
	}
	for i := range boards {
		if c, ok := counts[boards[i].ID]; ok {
			boards[i].Counts = c
		}
	}
	return boards, nil
}

func (s *Store) GetBoard(id int64) (Board, error) {
	b := Board{ID: id, Folders: []string{}, Counts: zeroCounts()}
	err := s.DB.QueryRow(`SELECT name,description FROM boards WHERE id=?`, id).Scan(&b.Name, &b.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return Board{}, ErrNotFound
	}
	if err != nil {
		return Board{}, err
	}
	rows, err := s.DB.Query(`SELECT path FROM board_folders WHERE board_id=? ORDER BY position`, id)
	if err != nil {
		return Board{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return Board{}, err
		}
		b.Folders = append(b.Folders, p)
	}
	if err := rows.Err(); err != nil {
		return Board{}, err
	}
	rows.Close() // single connection: release before the counts query
	counts, err := s.cardCounts(id)
	if err != nil {
		return Board{}, err
	}
	if c, ok := counts[id]; ok {
		b.Counts = c
	}
	return b, nil
}

func (s *Store) UpdateBoard(id int64, name, description string, folders []string) (Board, error) {
	name, err := cleanName(name)
	if err != nil {
		return Board{}, err
	}
	description, err = cleanDescription(description)
	if err != nil {
		return Board{}, err
	}
	folders, err = cleanFolders(folders)
	if err != nil {
		return Board{}, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Board{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE boards SET name=?, description=? WHERE id=?`, name, description, id)
	if err != nil {
		return Board{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Board{}, ErrNotFound
	}
	if _, err := tx.Exec(`DELETE FROM board_folders WHERE board_id=?`, id); err != nil {
		return Board{}, err
	}
	if err := saveFolders(tx, id, folders); err != nil {
		return Board{}, err
	}
	if err := tx.Commit(); err != nil {
		return Board{}, err
	}
	return s.GetBoard(id)
}

func (s *Store) DeleteBoard(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM boards WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
