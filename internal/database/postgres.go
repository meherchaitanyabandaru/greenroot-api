package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
)

func Open(ctx context.Context, cfg config.DatabaseConfig) (*sql.DB, error) {
	if cfg.URL == "" {
		return sql.OpenDB(noopConnector{}), nil
	}

	db, err := sql.Open("pgx", cfg.URL)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

type noopConnector struct{}

func (noopConnector) Connect(context.Context) (driver.Conn, error) {
	return noopConn{}, nil
}

func (noopConnector) Driver() driver.Driver {
	return noopDriver{}
}

type noopDriver struct{}
type noopConn struct{}
type noopStmt struct{}
type noopTx struct{}
type noopRows struct{}

func (noopDriver) Open(string) (driver.Conn, error)         { return noopConn{}, nil }
func (noopConn) Prepare(string) (driver.Stmt, error)        { return noopStmt{}, nil }
func (noopConn) Close() error                               { return nil }
func (noopConn) Begin() (driver.Tx, error)                  { return noopTx{}, nil }
func (noopStmt) Close() error                               { return nil }
func (noopStmt) NumInput() int                              { return 0 }
func (noopStmt) Exec([]driver.Value) (driver.Result, error) { return driver.RowsAffected(0), nil }
func (noopStmt) Query([]driver.Value) (driver.Rows, error)  { return noopRows{}, nil }
func (noopTx) Commit() error                                { return nil }
func (noopTx) Rollback() error                              { return nil }
func (noopRows) Columns() []string                          { return []string{} }
func (noopRows) Close() error                               { return nil }
func (noopRows) Next([]driver.Value) error                  { return io.EOF }
