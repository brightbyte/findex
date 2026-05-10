package diff

import (
	"context"
	"database/sql"
	"findex/pkg/recorder"
	"findex/pkg/scanner"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/sync/errgroup"
	_ "modernc.org/sqlite"
)

type Differ struct {
	IncludeDotFiles bool
	Recursive       bool
	Output          io.Writer // progress output (nil = silent)
}

func (d *Differ) Diff(dir string) error {
	db, err := sql.Open("sqlite", filepath.Join(dir, ".findex.sqlite"))
	if err != nil {
		return err
	}
	defer db.Close()

	rec := &recorder.Sqlite{Temp: true, DB: db, BatchSize: 500}
	s := scanner.New()
	s.IncludeDotFiles = d.IncludeDotFiles
	s.Recursive = d.Recursive
	s.Output = d.Output

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	errs, ctx := errgroup.WithContext(ctx)
	ch := make(chan *recorder.Record)
	errs.Go(func() error {
		defer close(ch)
		return s.Scan(ctx, dir, ch)
	})
	errs.Go(func() error {
		return recorder.Consume(dir, rec, ch)
	})
	if err := errs.Wait(); err != nil {
		return err
	}

	return printDiff(db)
}

func printDiff(db *sql.DB) error {
	if err := queryAdded(db); err != nil {
		return err
	}
	if err := queryRemoved(db); err != nil {
		return err
	}
	return queryModified(db)
}

func queryAdded(db *sql.DB) error {
	rows, err := db.Query(`
		SELECT path, basename FROM scan_files
		WHERE (path, basename) NOT IN (SELECT path, basename FROM files)
		ORDER BY path, basename
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var path, basename string
		if err := rows.Scan(&path, &basename); err != nil {
			return err
		}
		fmt.Printf("[added]    %s\n", filepath.Join(path, basename))
	}
	return rows.Err()
}

func queryRemoved(db *sql.DB) error {
	rows, err := db.Query(`
		SELECT path, basename FROM files
		WHERE (path, basename) NOT IN (SELECT path, basename FROM scan_files)
		ORDER BY path, basename
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var path, basename string
		if err := rows.Scan(&path, &basename); err != nil {
			return err
		}
		fmt.Printf("[removed]  %s\n", filepath.Join(path, basename))
	}
	return rows.Err()
}

func queryModified(db *sql.DB) error {
	rows, err := db.Query(`
		SELECT f.path, f.basename, f.size, s.size
		FROM files f JOIN scan_files s ON f.path = s.path AND f.basename = s.basename
		WHERE f.size != s.size
		ORDER BY f.path, f.basename
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var path, basename string
		var oldSize, newSize int64
		if err := rows.Scan(&path, &basename, &oldSize, &newSize); err != nil {
			return err
		}
		fmt.Printf("[modified] %s (%d -> %d)\n", filepath.Join(path, basename), oldSize, newSize)
	}
	return rows.Err()
}
