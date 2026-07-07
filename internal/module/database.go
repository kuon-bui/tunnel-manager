package module

import (
	"database/sql"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"go.uber.org/fx"
	_ "modernc.org/sqlite"

	appconfig "tunnelmanager/internal/infrastructure/config"
)

func NewSQLDB(cfg appconfig.Config) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+cfg.DBPath+"?cache=shared&mode=rwc")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func NewBunDB(sqldb *sql.DB) *bun.DB {
	return bun.NewDB(sqldb, sqlitedialect.New())
}

var Database = fx.Module("database",
	fx.Provide(
		NewSQLDB,
		NewBunDB,
	),
)
