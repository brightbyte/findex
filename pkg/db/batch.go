package db

import (
	"database/sql"
)

type Batch struct {
	BatchSize int
	DB        *Database
	Prepared  *sql.Stmt
	tx        *sql.Tx
	stmt      *sql.Stmt
	count     int
}

func (b *Batch) begin() error {
	if b.tx != nil {
		panic("Transaction already in progress")
	}

	tx, err := b.DB.conn.Begin()
	if err != nil {
		return err
	}
	b.tx = tx
	b.stmt = tx.Stmt(b.Prepared)
	b.count = 0
	return nil
}

func (b *Batch) Commit() error {
	if b.tx == nil {
		return nil
	}

	_ = b.stmt.Close()
	err := b.tx.Commit()
	b.tx = nil
	return err
}

func (b *Batch) Rollback() error {
	if b.tx == nil {
		return nil
	}

	_ = b.stmt.Close()
	err := b.tx.Rollback()
	b.tx = nil
	return err
}

func (b *Batch) Exec(values ...any) (sql.Result, error) {
	res, err := b.stmt.Exec(values...)
	if err != nil {
		return nil, err
	}
	b.count++
	if b.count >= b.BatchSize {
		if err := b.Commit(); err != nil {
			return nil, err
		}
		return nil, b.begin()
	}
	return res, nil
}
