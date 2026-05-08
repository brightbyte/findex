package recorder

import (
	"database/sql"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Sqlite struct {
	db *sql.DB
}

func (s *Sqlite) Open(basepath string) error {
	db, err := sql.Open("sqlite", filepath.Join(basepath, ".findex.sqlite"))
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			path     TEXT    NOT NULL,
			basename TEXT    NOT NULL,
			size     INTEGER NOT NULL
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_path_basename ON files(path, basename);
		CREATE INDEX IF NOT EXISTS idx_size ON files(size);
	`)
	if err != nil {
		_ = db.Close()
		return err
	}

	s.db = db
	return nil
}

func (s *Sqlite) Record(rec *Record) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO files(path, basename, size) VALUES(?, ?, ?)`,
		filepath.Dir(rec.Path),
		filepath.Base(rec.Path),
		rec.Info.Size(),
	)
	return err
}

func (s *Sqlite) Close(err error) error {
	return s.db.Close()
}
