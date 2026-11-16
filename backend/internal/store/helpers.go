package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrConflict is returned for unique-constraint violations.
var ErrConflict = errors.New("conflict")

// nullableID maps an empty string to a SQL NULL uuid.
func nullableID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// mapConstraint converts PostgreSQL unique violations into ErrConflict.
func mapConstraint(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}
