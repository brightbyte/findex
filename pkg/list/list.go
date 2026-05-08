package list

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func List(dir, prefix, glob string) error {
	db, err := sql.Open("sqlite", filepath.Join(dir, ".findex.sqlite"))
	if err != nil {
		return err
	}
	defer db.Close()

	var rows *sql.Rows
	if prefix == "" {
		rows, err = db.Query(`SELECT id, path, basename, size FROM files ORDER BY path, basename`)
	} else {
		rows, err = db.Query(
			`SELECT id, path, basename, size FROM files WHERE path = ? OR path LIKE ? ORDER BY path, basename`,
			prefix, prefix+"/%",
		)
	}
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, size int64
		var path, basename string
		if err := rows.Scan(&id, &path, &basename, &size); err != nil {
			return err
		}
		if glob != "" {
			if matched, err := filepath.Match(glob, basename); err != nil {
				return err
			} else if !matched {
				continue
			}
		}
		fmt.Printf("%s/%s  %d bytes\n", path, basename, size)
	}
	return rows.Err()
}
