package db

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
)

//go:embed migration/*.sql
var migration embed.FS

// Migrate applies ethrd migrations to db, leaving it open for subsequent use.
func Migrate(logger *slog.Logger, db *sql.DB) error {
	start := time.Now()
	dir, err := fs.Sub(migration, "migration")
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, dir, goose.WithSlog(logger))
	if err != nil {
		return err
	}
	if _, err := p.Up(context.Background()); err != nil {
		return err
	}
	version, err := p.GetDBVersion(context.Background())
	if err != nil {
		return err
	}
	logger.Info("migrate",
		"status", "complete",
		"version", version,
		"delta_time", time.Since(start))
	return nil
}
