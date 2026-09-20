package postgres

import (
	"errors"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return domain.ErrAlreadyExists
		case "23503", "23514", "22001", "22P02":
			return domain.ErrValidation
		}
	}
	return err
}
