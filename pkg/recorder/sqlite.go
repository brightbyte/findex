package recorder

import (
	"database/sql"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Sqlite struct {
	Filename  string
	BatchSize int
	db        *sql.DB
	prepared  *sql.Stmt
	tx        *sql.Tx
	stmt      *sql.Stmt
	count     int
}

func (s *Sqlite) batchSize() int {
	if s.BatchSize < 1 {
		panic("Sqlite.BatchSize must be >= 1")
	}
	return s.BatchSize
}

func (s *Sqlite) Open(basepath string) error {
	db, err := sql.Open("sqlite", filepath.Join(basepath, s.Filename))
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

	prepared, err := db.Prepare(`INSERT OR REPLACE INTO files(path, basename, size) VALUES(?, ?, ?)`)
	if err != nil {
		_ = db.Close()
		return err
	}

	s.db = db
	s.prepared = prepared
	return s.beginBatch()
}

func (s *Sqlite) beginBatch() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	s.tx = tx
	s.stmt = tx.Stmt(s.prepared)
	s.count = 0
	return nil
}

func (s *Sqlite) commitBatch() error {
	_ = s.stmt.Close()
	return s.tx.Commit()
}

func (s *Sqlite) Record(rec *Record) error {
	_, err := s.stmt.Exec(
		filepath.Dir(rec.Path),
		filepath.Base(rec.Path),
		rec.Info.Size(),
	)
	if err != nil {
		return err
	}
	s.count++
	if s.count >= s.batchSize() {
		if err := s.commitBatch(); err != nil {
			return err
		}
		return s.beginBatch()
	}
	return nil
}

func (s *Sqlite) Close(err error) error {
	if err != nil {
		_ = s.stmt.Close()
		_ = s.tx.Rollback()
		_ = s.prepared.Close()
		return s.db.Close()
	}
	if commitErr := s.commitBatch(); commitErr != nil {
		_ = s.prepared.Close()
		_ = s.db.Close()
		return commitErr
	}
	_ = s.prepared.Close()
	return s.db.Close()
}
