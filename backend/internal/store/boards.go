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
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Folders []string `json:"folders"`
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return "", ValidationError{"name must be 1-100 characters"}
	}
	return name, nil
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

func (s *Store) CreateBoard(name string, folders []string) (Board, error) {
	name, err := cleanName(name)
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
	res, err := tx.Exec(`INSERT INTO boards(name) VALUES(?)`, name)
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
	return Board{ID: id, Name: name, Folders: folders}, nil
}

func (s *Store) ListBoards() ([]Board, error) {
	boards := []Board{}
	idx := map[int64]int{}
	rows, err := s.DB.Query(`SELECT id,name FROM boards ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		b := Board{Folders: []string{}}
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
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
	return boards, frows.Err()
}

func (s *Store) GetBoard(id int64) (Board, error) {
	b := Board{ID: id, Folders: []string{}}
	err := s.DB.QueryRow(`SELECT name FROM boards WHERE id=?`, id).Scan(&b.Name)
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
	return b, rows.Err()
}

func (s *Store) UpdateBoard(id int64, name string, folders []string) (Board, error) {
	name, err := cleanName(name)
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
	res, err := tx.Exec(`UPDATE boards SET name=? WHERE id=?`, name, id)
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
	return Board{ID: id, Name: name, Folders: folders}, nil
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
