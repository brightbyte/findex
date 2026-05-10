package recorder

import (
	"findex/pkg/db"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type DBRecorder struct {
	BatchSize int
	DB        *db.Database
	timestamp time.Time
	batch     *db.Batch
}

func (s *DBRecorder) batchSize() int {
	if s.BatchSize < 1 {
		panic("Sqlite.BatchSize must be >= 1")
	}
	return s.BatchSize
}

func (s *DBRecorder) Open(basepath string) error {
	s.timestamp = time.Now()

	var err error
	_, err = s.DB.Exec(
		`CREATE {TABLE} IF NOT EXISTS 
			{prefix}files (
				id        INTEGER PRIMARY KEY AUTOINCREMENT,
				path      TEXT    NOT NULL,
				basename  TEXT    NOT NULL,
				size      INTEGER NOT NULL,
				timestamp INTEGER NOT NULL
			);
			CREATE UNIQUE INDEX IF NOT EXISTS {prefix}idx_path_basename ON {prefix}files (path, basename);
			CREATE INDEX IF NOT EXISTS {prefix}idx_size ON {prefix}files (size);
			CREATE INDEX IF NOT EXISTS {prefix}idx_timestamp ON {prefix}files (timestamp);
		`)
	if err != nil {
		return err
	}

	prepared, err := s.DB.Prepare(`INSERT OR REPLACE INTO {prefix}files (path, basename, size, timestamp) VALUES(?, ?, ?, ?)`)
	if err != nil {
		return err
	}

	s.batch, err = s.DB.BeginBatch(prepared, s.batchSize())
	return err
}

func (s *DBRecorder) Record(rec *Record) error {
	_, err := s.batch.Exec(
		filepath.Dir(rec.Path),
		filepath.Base(rec.Path),
		rec.Info.Size(),
		s.timestamp.Unix(),
	)
	return err
}

func (s *DBRecorder) Close(err error) error {
	if err != nil {
		rollbackErr := s.batch.Rollback()
		return rollbackErr
	}

	if commitErr := s.batch.Commit(); commitErr != nil {
		return commitErr
	}

	// TODO: skip prune and digest when used in diff mode (DB.temp == true);
	// they operate on temp tables only and produce unwanted aggregation output.
	if pruneErr := s.prune(); pruneErr != nil {
		return pruneErr
	}

	if digestErr := s.digest(); digestErr != nil {
		return digestErr
	}

	return nil
}

// delete outdated entries  from the file table
func (s *DBRecorder) prune() error {
	_, err := s.DB.Exec(`DELETE FROM {prefix}files WHERE timestamp <> ?`, s.timestamp.Unix())
	return err
}

// generate digest of directories
func (s *DBRecorder) digest() error {
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
func (s *DBRecorder) buildDirSummaries() error {
	_, err := s.DB.Exec(`
		CREATE {TABLE} IF NOT EXISTS {prefix}dir_summaries (
			path       TEXT    PRIMARY KEY NOT NULL,
			file_count INTEGER NOT NULL,
			total_size INTEGER NOT NULL,
			depth      INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS {prefix}idx_size_count ON {prefix}dir_summaries(total_size, file_count)
	`)

	if err != nil {
		return err
	}

	_, err = s.DB.Exec(`DELETE FROM {prefix}dir_summaries`)
	if err != nil {
		return err
	}

	rows, err := s.DB.Query(`
		SELECT path, COUNT(*), SUM(size),
		       LENGTH(path) - LENGTH(REPLACE(path, '/', ''))
		FROM {prefix}files
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
		_, err = s.DB.Exec(
			`INSERT INTO {prefix}dir_summaries(path, file_count, total_size, depth) VALUES(?, ?, ?, ?)`,
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
func (s *DBRecorder) loadDirSummaries() ([]dirSummary, error) {
	rows, err := s.DB.Query(`SELECT path, file_count, total_size, depth FROM {prefix}dir_summaries ORDER BY depth DESC`)
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
func (s *DBRecorder) aggregateDirSummaries(dirs []dirSummary) error {
	stmt, err := s.DB.Prepare(`
		UPDATE {prefix}dir_summaries SET file_count = file_count + ?, total_size = total_size + ? WHERE path = ?
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
