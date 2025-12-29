package store

import (
	"context"
	"database/sql"
)


// sqlExecutor is implemented by *sql.DB and *sql.Tx so store methods can
// run against either a direct connection or an explicit transaction.
type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
