package diff

import (
	"context"
	"findex/pkg/db"
	"findex/pkg/recorder"
	"findex/pkg/scanner"
	"fmt"
	"io"
	"path/filepath"
)

type Differ struct {
	OldState        *db.Database
	IncludeDotFiles bool
	Recursive       bool
	BatchSize       int
	Output          io.Writer // progress output (nil = silent)
	CurrentState    *db.Database
}

func (d *Differ) Diff(ctx context.Context, dir string) error {
	d.CurrentState = d.OldState.WithPrefix("diff_", true)

	rec := &recorder.DBRecorder{DB: d.CurrentState, BatchSize: d.BatchSize}
	s := scanner.New()
	s.IncludeDotFiles = d.IncludeDotFiles
	s.Recursive = d.Recursive
	s.Output = d.Output // TODO: use this instead of fmt.Printf below

	err := s.ScanInto(ctx, dir, rec)
	if err != nil {
		return err
	}

	return d.printDiff()
}

func (d *Differ) printDiff() error {
	if err := d.queryAdded(); err != nil {
		return err
	}
	if err := d.queryRemoved(); err != nil {
		return err
	}
	return d.queryModified()
}

func (d *Differ) queryAdded() error {
	rows, err := d.OldState.Query(`
		SELECT path, basename FROM diff_files
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

func (d *Differ) queryRemoved() error {
	rows, err := d.OldState.Query(`
		SELECT path, basename FROM files
		WHERE (path, basename) NOT IN (SELECT path, basename FROM diff_files)
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

func (d *Differ) queryModified() error {
	rows, err := d.OldState.Query(`
		SELECT f.path, f.basename, f.size, s.size
		FROM files f JOIN diff_files s ON f.path = s.path AND f.basename = s.basename
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
