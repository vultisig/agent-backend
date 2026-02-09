package postgres

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/sirupsen/logrus"
)

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	db := &DB{pool: pool}
	if err := db.Migrate(); err != nil {
		pool.Close()
		return nil, err
	}

	return db, nil
}

func (d *DB) Migrate() error {
	logrus.Info("Starting database migration...")

	goose.SetLogger(logrus.StandardLogger())
	goose.SetBaseFS(migrations)
	defer goose.SetBaseFS(nil)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	db := stdlib.OpenDBFromPool(d.pool)
	defer db.Close()

	if err := goose.Up(db, "migrations", goose.WithAllowMissing()); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	logrus.Info("Database migration completed")
	return nil
}

func (d *DB) Pool() *pgxpool.Pool {
	return d.pool
}

func (d *DB) Close() {
	d.pool.Close()
}
