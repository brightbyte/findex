package dirdupes

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	_ "modernc.org/sqlite"
)

type fileEntry struct {
	name string
	size int64
}

type Detector struct {
	dir        string
	db         *sql.DB
	MinSize    int64 // minimum total_size in bytes for a directory to be considered
	entryCache map[string][]fileEntry
	covered    map[string]bool
}

func NewDetector(dir string) *Detector {
	return &Detector{
		dir:        dir,
		entryCache: make(map[string][]fileEntry),
		covered:    make(map[string]bool),
	}
}

func (d *Detector) DetectDupes() error {
	db, err := sql.Open("sqlite", filepath.Join(d.dir, ".findex.sqlite"))
	if err != nil {
		return err
	}
	defer db.Close()
	d.db = db
	return d.findDirDupes()
}

// findDirDupes finds directories with matching (total_size, file_count),
// then confirms with a sorted file-size multiset comparison.
// Candidates are processed in ascending depth order; once a directory is
// identified as a duplicate, its subdirectories are skipped.
func (d *Detector) findDirDupes() error {
	rows, err := d.db.Query(`
		SELECT path, file_count, total_size, depth FROM dir_summaries
		WHERE file_count > 0
		  AND total_size >= ?
		  AND (total_size, file_count) IN (
		      SELECT total_size, file_count FROM dir_summaries
		      WHERE file_count > 0
		        AND total_size >= ?
		      GROUP BY total_size, file_count
		      HAVING COUNT(*) > 1
		  )
		ORDER BY depth, total_size, file_count, path
	`, d.MinSize, d.MinSize)
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
				if sameInode(filepath.Join(d.dir, pi), filepath.Join(d.dir, pj)) {
					fmt.Printf("[hardlink] %s == %s\n", pi, pj)
					d.covered[pi] = true
					d.covered[pj] = true
					continue
				}
				si, err := d.fileEntries(pi)
				if err != nil {
					return err
				}
				sj, err := d.fileEntries(pj)
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
func (d *Detector) fileEntries(path string) ([]fileEntry, error) {
	if e, ok := d.entryCache[path]; ok {
		return e, nil
	}

	fileRows, err := d.db.Query(
		`SELECT basename, size FROM files WHERE path = ?`,
		path,
	)
	if err != nil {
		return nil, err
	}
	var entries []fileEntry
	for fileRows.Next() {
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
	subdirRows, err := d.db.Query(
		`SELECT path, total_size FROM dir_summaries
		 WHERE path LIKE ? AND path NOT LIKE ?`,
		subdirPattern, subdirPattern+"/%",
	)
	if err != nil {
		return nil, err
	}
	for subdirRows.Next() {
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
