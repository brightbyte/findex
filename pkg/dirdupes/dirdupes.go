package dirdupes

import (
	"context"
	"findex/pkg/db"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

type fileEntry struct {
	name string
	size int64
}

type Detector struct {
	DB         *db.Database
	MinSize    int64 // minimum total_size in bytes for a directory to be considered
	entryCache map[string][]fileEntry
	covered    map[string]bool
}

func NewDetector(db *db.Database) *Detector {
	return &Detector{
		DB:         db,
		entryCache: make(map[string][]fileEntry),
		covered:    make(map[string]bool),
	}
}

// DetectDupes finds directories with matching (total_size, file_count),
// then confirms with a sorted file-size multiset comparison.
// Candidates are processed in ascending depth order; once a directory is
// identified as a duplicate, its subdirectories are skipped.
func (d *Detector) DetectDupes(ctx context.Context, dir string) error {
	rows, err := d.DB.Query(`
		SELECT DISTINCT d.path, d.file_count, d.total_size, d.depth
		FROM {prefix}dir_summaries d
		JOIN {prefix}dir_summaries d2
		  ON d.total_size = d2.total_size
		 AND d.file_count = d2.file_count
		 AND d.path != d2.path
		WHERE d.file_count > 0
		  AND d.total_size >= ?
		ORDER BY d.depth, d.total_size, d.file_count, d.path
	`, d.MinSize)
	if err != nil {
		return err
	}

	type key struct{ size, count int64 }
	type candidate struct {
		path  string
		depth int
	}
	groups := make(map[key][]candidate)
	var keys []key

	for rows.Next() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		var path string
		var fileCount, totalSize int64
		var depth int
		if err := rows.Scan(&path, &fileCount, &totalSize, &depth); err != nil {
			rows.Close()
			return err
		}
		k := key{totalSize, fileCount}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], candidate{path, depth})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, k := range keys {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		candidates := groups[k]
		for i := 0; i < len(candidates); i++ {
			if d.isCovered(candidates[i].path) {
				continue
			}
			for j := i + 1; j < len(candidates); j++ {
				if d.isCovered(candidates[j].path) {
					continue
				}
				pi, pj := candidates[i].path, candidates[j].path
				if sameInode(filepath.Join(dir, pi), filepath.Join(dir, pj)) {
					fmt.Printf("[hardlink] %s == %s\n", pi, pj)
					d.covered[pi] = true
					d.covered[pj] = true
					continue
				}
				si, err := d.fileEntries(ctx, pi)
				if err != nil {
					return err
				}
				sj, err := d.fileEntries(ctx, pj)
				if err != nil {
					return err
				}
				if slices.Equal(si, sj) {
					fmt.Printf("[dirdup] %s == %s\n", pi, pj)
					d.covered[pi] = true
					d.covered[pj] = true
				}
			}
		}
	}
	return nil
}

// isCovered returns true if path or any of its ancestors has been marked as covered.
func (d *Detector) isCovered(path string) bool {
	p := path
	for {
		if d.covered[p] {
			return true
		}
		parent := parentPath(p)
		if parent == "" {
			return false
		}
		p = parent
	}
}

// fileEntries returns a sorted slice of (basename, size) pairs for all own files
// in the directory, plus one entry per direct subdirectory using its name and total_size.
func (d *Detector) fileEntries(ctx context.Context, path string) ([]fileEntry, error) {
	if e, ok := d.entryCache[path]; ok {
		return e, nil
	}

	fileRows, err := d.DB.Query(
		`SELECT basename, size FROM {prefix}files WHERE path = ?`,
		path,
	)
	if err != nil {
		return nil, err
	}
	var entries []fileEntry
	for fileRows.Next() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var e fileEntry
		if err := fileRows.Scan(&e.name, &e.size); err != nil {
			fileRows.Close()
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := fileRows.Close(); err != nil {
		return nil, err
	}

	var subdirPattern string
	if path == "." {
		subdirPattern = "%"
	} else {
		subdirPattern = path + "/%"
	}
	subdirRows, err := d.DB.Query(
		`SELECT path, total_size FROM {prefix}dir_summaries
		 WHERE path LIKE ? AND path NOT LIKE ?`,
		subdirPattern, subdirPattern+"/%",
	)
	if err != nil {
		return nil, err
	}
	for subdirRows.Next() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var subpath string
		var size int64
		if err := subdirRows.Scan(&subpath, &size); err != nil {
			subdirRows.Close()
			return nil, err
		}
		entries = append(entries, fileEntry{
			name: subpath[strings.LastIndex(subpath, "/")+1:],
			size: size,
		})
	}
	if err := subdirRows.Close(); err != nil {
		return nil, err
	}

	slices.SortFunc(entries, func(a, b fileEntry) int {
		return strings.Compare(a.name, b.name)
	})
	d.entryCache[path] = entries
	return entries, nil
}

// sameInode returns true if two paths refer to the same inode on the same device.
func sameInode(a, b string) bool {
	sa, err := os.Lstat(a)
	if err != nil {
		return false
	}
	sb, err := os.Lstat(b)
	if err != nil {
		return false
	}
	stata, ok := sa.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	statb, ok := sb.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return stata.Ino == statb.Ino && stata.Dev == statb.Dev
}

// parentPath returns the parent directory path, or "" for the root ".".
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
