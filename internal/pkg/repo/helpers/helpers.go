package helpers

import (
	"database/sql"
	"errors"
	"tunnelmanager/internal/model"
)

func MapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrNotFound
	}
	return err
}
