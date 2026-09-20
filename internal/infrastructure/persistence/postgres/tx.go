package postgres

import (
	"context"

	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type transactionKey struct{}

type DBRunner interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func runner(ctx context.Context, pool *pgxpool.Pool) DBRunner {
	if tx, ok := ctx.Value(transactionKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

type TransactionManager struct {
	Pool *pgxpool.Pool
}

func (m TransactionManager) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	if _, ok := ctx.Value(transactionKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	tx, err := m.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txCtx := context.WithValue(ctx, transactionKey{}, tx)
	if err := fn(txCtx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var _ repository.TransactionManager = (*TransactionManager)(nil)
