package recorder

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Sqlite struct {
	Filename  string
	BatchSize int
	Temp      bool    // write to a TEMP TABLE "scan_files" instead of "files"; skip prune/digest
	DB        *sql.DB // if non-nil, use this connection instead of opening Filename; not closed on Close
	timestamp time.Time
	db        *sql.DB
	prepared  *sql.Stmt
	tx        *sql.Tx
	stmt      *sql.Stmt
	count     int
	ownDB     bool // true when we opened the DB ourselves and must close it
}

func (s *Sqlite) batchSize() int {
	if s.BatchSize < 1 {
		panic("Sqlite.BatchSize must be >= 1")
	}
	return s.BatchSize
}

func (s *Sqlite) Open(basepath string) error {
	s.timestamp = time.Now()

	if s.DB != nil {
		s.db = s.DB
		s.ownDB = false
	} else {
		db, err := sql.Open("sqlite", filepath.Join(basepath, s.Filename))
		if err != nil {
			return err
		}
		s.db = db
		s.ownDB = true
	}

	var err error
	if s.Temp {
		_, err = s.db.Exec(`
			CREATE TEMP TABLE IF NOT EXISTS scan_files (
				path      TEXT    NOT NULL,
				basename  TEXT    NOT NULL,
				size      INTEGER NOT NULL
			);
			CREATE UNIQUE INDEX IF NOT EXISTS idx_scan_path_basename ON scan_files(path, basename);
		`)
	} else {
		_, err = s.db.Exec(`
			CREATE TABLE IF NOT EXISTS files (
				id        INTEGER PRIMARY KEY AUTOINCREMENT,
				path      TEXT    NOT NULL,
				basename  TEXT    NOT NULL,
				size      INTEGER NOT NULL,
				timestamp INTEGER NOT NULL
			);
			CREATE UNIQUE INDEX IF NOT EXISTS idx_path_basename ON files(path, basename);
			CREATE INDEX IF NOT EXISTS idx_size ON files(size);
			CREATE INDEX IF NOT EXISTS idx_timestamp ON files(timestamp);
		`)
	}
	if err != nil {
		if s.ownDB {
			_ = s.db.Close()
		}
		return err
	}

	var prepared *sql.Stmt
	if s.Temp {
		prepared, err = s.db.Prepare(`INSERT OR REPLACE INTO scan_files(path, basename, size) VALUES(?, ?, ?)`)
	} else {
		prepared, err = s.db.Prepare(`INSERT OR REPLACE INTO files(path, basename, size, timestamp) VALUES(?, ?, ?, ?)`)
	}
	if err != nil {
		if s.ownDB {
			_ = s.db.Close()
		}
		return err
	}

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
	var err error
	if s.Temp {
		_, err = s.stmt.Exec(
			filepath.Dir(rec.Path),
			filepath.Base(rec.Path),
			rec.Info.Size(),
		)
	} else {
		_, err = s.stmt.Exec(
			filepath.Dir(rec.Path),
			filepath.Base(rec.Path),
			rec.Info.Size(),
			s.timestamp.Unix(),
		)
	}
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
	if s.db == nil {
		return nil
	}

	closeDB := func() error {
		if s.ownDB {
			return s.db.Close()
		}
		return nil
	}

	if err != nil {
		if s.stmt != nil {
			_ = s.stmt.Close()
		}
		if s.tx != nil {
			_ = s.tx.Rollback()
		}
		if s.prepared != nil {
			_ = s.prepared.Close()
		}
		return closeDB()
	}

	if commitErr := s.commitBatch(); commitErr != nil {
		_ = s.prepared.Close()
		_ = closeDB()
		return commitErr
	}

	_ = s.prepared.Close()

	if s.Temp {
		return closeDB()
	}

	if pruneErr := s.prune(); pruneErr != nil {
		_ = closeDB()
		return pruneErr
	}

	if digestErr := s.digest(); digestErr != nil {
		_ = closeDB()
		return digestErr
	}

	return closeDB()
}

// delete outdated entries  from the file table
func (s *Sqlite) prune() error {
	_, err := s.db.Exec(fmt.Sprintf(`DELETE FROM files WHERE timestamp <> %d`, s.timestamp.Unix()))
	return err
}

// generate digest of directories
func (s *Sqlite) digest() error {
	if err := s.buildDirSummaries(); err != nil {
		return err
	}

	dirs, err := s.loadDirSummaries()
	if err != nil {
		return err
	}

	if err := s.aggregateDirSummaries(dirs); err != nil {
		return err
	}

	return nil
}

type dirSummary struct {
	path      string
	fileCount int64
	totalSize int64
	depth     int
}

// buildDirSummaries creates the dir_summaries table and populates it with
// per-directory file counts and sizes from the files table (own files only).
func (s *Sqlite) buildDirSummaries() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS dir_summaries (
			path       TEXT    PRIMARY KEY NOT NULL,
			file_count INTEGER NOT NULL,
			total_size INTEGER NOT NULL,
			depth      INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_size_count ON dir_summaries(total_size, file_count)
	`)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`DELETE FROM dir_summaries`)
	if err != nil {
		return err
	}

	rows, err := s.db.Query(`
		SELECT path, COUNT(*), SUM(size),
		       LENGTH(path) - LENGTH(REPLACE(path, '/', ''))
		FROM files
		GROUP BY path
	`)
	if err != nil {
		return err
	}

	var dirs []dirSummary
	for rows.Next() {
		var s dirSummary
		if err := rows.Scan(&s.path, &s.fileCount, &s.totalSize, &s.depth); err != nil {
			rows.Close()
			return err
		}
		dirs = append(dirs, s)
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, d := range dirs {
		_, err = s.db.Exec(
			`INSERT INTO dir_summaries(path, file_count, total_size, depth) VALUES(?, ?, ?, ?)`,
			d.path, d.fileCount, d.totalSize, d.depth,
		)
		if err != nil {
			return err
		}
		// fmt.Printf("[pass1] %s: %d files, %d bytes\n", s.path, s.fileCount, s.totalSize)
	}
	return nil
}

// loadDirSummaries returns all directories ordered by depth descending.
func (s *Sqlite) loadDirSummaries() ([]dirSummary, error) {
	rows, err := s.db.Query(`SELECT path, file_count, total_size, depth FROM dir_summaries ORDER BY depth DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dirs []dirSummary
	for rows.Next() {
		var d dirSummary
		if err := rows.Scan(&d.path, &d.fileCount, &d.totalSize, &d.depth); err != nil {
			return nil, err
		}
		dirs = append(dirs, d)
	}
	return dirs, rows.Err()
}

// aggregateDirSummaries propagates each directory's size into its direct parent, deepest first.
func (s *Sqlite) aggregateDirSummaries(dirs []dirSummary) error {
	stmt, err := s.db.Prepare(`
		UPDATE dir_summaries SET file_count = file_count + ?, total_size = total_size + ? WHERE path = ?
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, d := range dirs {
		parent := parentPath(d.path)
		if parent == "" {
			continue
		}
		if _, err := stmt.Exec(d.fileCount, d.totalSize, parent); err != nil {
			return err
		}
		fmt.Printf("%s  aggregated (%d files, +%d bytes)\n", d.path, d.fileCount, d.totalSize)
	}
	return nil
}

// parentPath returns the parent directory path, or "" for the root ("." or "/").
func parentPath(path string) string {
	if path == "." || path == "/" {
		return ""
	}
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return "."
	}
	return path[:i]
}
