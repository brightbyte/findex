package dupes

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/cespare/xxhash/v2"
	_ "modernc.org/sqlite"
)

type fileEntry struct {
	relPath string
	absPath string
	size    int64
	inode   uint64
	dev     uint64
	hasIno  bool
}

func Dupes(dir string) error {
	db, err := sql.Open("sqlite", filepath.Join(dir, ".findex.sqlite"))
	if err != nil {
		return err
	}
	defer db.Close()

	groups, err := loadGroups(dir, db)
	if err != nil {
		return err
	}

	hashCache := make(map[string]uint64)
	for _, group := range groups {
		entries := statEntries(group)
		if len(entries) >= 2 {
			findDupes(entries, hashCache)
		}
	}
	return nil
}

// loadGroups queries the database and returns slices of fileEntry grouped by size.
// Groups with only one entry are excluded.
func loadGroups(dir string, db *sql.DB) ([][]fileEntry, error) {
	rows, err := db.Query(`
		SELECT path, basename, size FROM files
		WHERE size > 0
		ORDER BY size, path, basename
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups [][]fileEntry
	var current []fileEntry
	var currentSize int64 = -1

	for rows.Next() {
		var path, basename string
		var size int64
		if err := rows.Scan(&path, &basename, &size); err != nil {
			return nil, err
		}
		if size != currentSize {
			if len(current) > 1 {
				groups = append(groups, current)
			}
			current = nil
			currentSize = size
		}
		current = append(current, fileEntry{
			relPath: path + "/" + basename,
			absPath: filepath.Join(dir, path, basename),
			size:    size,
		})
	}
	if len(current) > 1 {
		groups = append(groups, current)
	}
	return groups, rows.Err()
}

// statEntries stats each file, skipping symlinks and missing files,
// and populates inode/dev where available.
func statEntries(group []fileEntry) []fileEntry {
	var entries []fileEntry
	for _, e := range group {
		info, err := os.Lstat(e.absPath)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			e.inode = stat.Ino
			e.dev = stat.Dev
			e.hasIno = true
		}
		entries = append(entries, e)
	}
	return entries
}

// findDupes prints hard link and content duplicate pairs within a group.
func findDupes(entries []fileEntry, hashCache map[string]uint64) {
	// Find hard link pairs by inode+dev.
	// inodeSeen tracks the first file seen for each inode; subsequent files with
	// the same inode are hard links and are skipped in the hash phase.
	inodeSeen := make(map[[2]uint64]string)
	skipInHash := make(map[string]bool)
	for _, e := range entries {
		if !e.hasIno {
			continue
		}
		key := [2]uint64{e.inode, e.dev}
		if first, ok := inodeSeen[key]; ok {
			fmt.Printf("[hardlink] %s == %s\n", first, e.relPath)
			skipInHash[e.relPath] = true
		} else {
			inodeSeen[key] = e.relPath
		}
	}

	// Find content duplicate pairs by xxhash.
	// Hard link duplicates (non-first members of an inode group) are skipped;
	// the first member of each inode group is still hashed so it can match
	// content duplicates in other inode groups.
	hashSeen := make(map[uint64]string)
	for _, e := range entries {
		if skipInHash[e.relPath] {
			continue
		}
		h, err := hashFile(e.absPath, hashCache)
		if err != nil {
			fmt.Printf("[error] %s\n", err)
			continue
		}
		if first, ok := hashSeen[h]; ok {
			fmt.Printf("[duplicate] %s == %s\n", first, e.relPath)
		} else {
			hashSeen[h] = e.relPath
		}
	}
}

func hashFile(path string, cache map[string]uint64) (uint64, error) {
	if h, ok := cache[path]; ok {
		return h, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	h := xxhash.New()
	if _, err := io.Copy(h, f); err != nil {
		return 0, err
	}
	result := h.Sum64()
	cache[path] = result
	return result, nil
}
