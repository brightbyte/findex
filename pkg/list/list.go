package list

import (
	"database/sql"
	"findex/pkg/db"
	"fmt"
	"path/filepath"
)

func List(db *db.Database, prefix, glob string) error {
	var err error
	var rows *sql.Rows
	if prefix == "" {
		rows, err = db.Query(`SELECT id, path, basename, size FROM {prefix}files ORDER BY path, basename`)
	} else {
		rows, err = db.Query(
			`SELECT id, path, basename, size FROM {prefix}files WHERE path = ? OR path LIKE ? ORDER BY path, basename`,
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
