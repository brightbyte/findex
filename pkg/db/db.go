package db

import (
	"database/sql"
	"strings"

	_ "modernc.org/sqlite"
)

type Database struct {
	prefix   string
	conn     *sql.DB
	temp     bool
	hydrator *strings.Replacer
}

func Open(file string) (*Database, error) {
	conn, err := sql.Open("sqlite", file)
	if err != nil {
		return nil, err
	}

	db := &Database{
		prefix: "",
		conn:   conn,
	}

	return db, err
}

func (db *Database) WithPrefix(prefix string, temp bool) *Database {
	dbWithPrefix := &Database{
		prefix: prefix,
		conn:   db.conn,
		temp:   temp,
	}

	return dbWithPrefix
}

func (db *Database) Name(name string) string {
	return db.prefix + name
}

func (db *Database) Table(name string, suffix string) string {
	if db.temp {
		return "TEMP TABLE " + db.Name(name) + " " + suffix
	} else {
		return "TABLE " + db.Name(name) + " " + suffix
	}
}

func (db *Database) Hydrate(query string) string {
	temp := ""
	if db.temp {
		temp = "TEMP"
	}

	if db.hydrator == nil {
		db.hydrator = strings.NewReplacer(
			"{prefix}", db.prefix,
			"{temp}", temp,
			"{TABLE}", temp+" TABLE",
		)
	}
	return db.hydrator.Replace(query)
}

func (db *Database) Exec(statement string, args ...any) (sql.Result, error) {
	return db.conn.Exec(db.Hydrate(statement), args...)
}

func (db *Database) Query(query string, args ...any) (*sql.Rows, error) {
	return db.conn.Query(db.Hydrate(query), args...)
}

func (db *Database) Prepare(query string) (*sql.Stmt, error) {
	return db.conn.Prepare(db.Hydrate(query))
}

func (db *Database) BeginBatch(prepared *sql.Stmt, batchSize int) (*Batch, error) {
	batch := &Batch{
		BatchSize: batchSize,
		DB:        db,
		Prepared:  prepared,
	}

	err := batch.begin()
	return batch, err
}

func (db *Database) Close() error {
	if db.conn == nil {
		return nil
	}
	connErr := db.conn.Close()
	db.conn = nil
	return connErr
}
