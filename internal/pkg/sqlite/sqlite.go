package sqlite

import (
	"database/sql"
	"tunnelmanager/internal/pkg/config"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"
)

func NewSQLDB(cfg config.Config) (*bun.DB, error) {
	db, err := sql.Open("sqlite", "file:"+cfg.DBPath+"?cache=shared&mode=rwc")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	return bun.NewDB(db, sqlitedialect.New()), nil
}
