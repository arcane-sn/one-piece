// Package store holds all SQLite access for One Piece.
package store

import (
	"database/sql"
	"errors"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

// ValidationError is a user-facing input problem (maps to HTTP 400).
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

// ConflictError is a valid request that the current state forbids (maps to HTTP 409).
type ConflictError struct{ Msg string }

func (e ConflictError) Error() string { return e.Msg }

type Store struct{ DB *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS users(
  id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sessions(
  token TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS boards(
  id INTEGER PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS board_folders(
  id INTEGER PRIMARY KEY, board_id INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
  path TEXT NOT NULL, position INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS cards(
  id INTEGER PRIMARY KEY, board_id INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
  title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', deadline TEXT NOT NULL DEFAULT '',
  approved INTEGER NOT NULL DEFAULT 0, content_version INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL, position INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS card_reviews(
  id INTEGER PRIMARY KEY, card_id INTEGER NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  content_version INTEGER NOT NULL, status TEXT NOT NULL,
  verdict TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '', missing TEXT NOT NULL DEFAULT '[]',
  log TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '');
`

// addColumn adds table.column (with the given DDL type/default) when it is missing.
func addColumn(db *sql.DB, table, column, ddl string) error {
	var has int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&has); err != nil {
		return err
	}
	if has > 0 {
		return nil
	}
	_, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + ddl)
	return err
}

// Open opens (creating if needed) the database at path and migrates it.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	// ponytail: one connection serializes writes; fine for a single-user tool.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	// Databases created before these columns existed lack them.
	for _, m := range [][3]string{
		{"boards", "description", "TEXT NOT NULL DEFAULT ''"},
		{"cards", "deadline", "TEXT NOT NULL DEFAULT ''"},
		{"cards", "approved", "INTEGER NOT NULL DEFAULT 0"},
		{"cards", "content_version", "INTEGER NOT NULL DEFAULT 1"},
	} {
		if err := addColumn(db, m[0], m[1], m[2]); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &Store{DB: db}, nil
}
