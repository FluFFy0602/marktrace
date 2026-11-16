// Package store owns the PostgreSQL connection, embedded migrations and seed data.
package store

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB is the minimal query interface shared by *pgxpool.Pool and pgx.Tx.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Connect opens a pool, retrying until the database answers or ctx/timeout expires.
func Connect(ctx context.Context, url string, timeout time.Duration) (*pgxpool.Pool, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		pool, err := pgxpool.New(ctx, url)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("database not reachable: %w", lastErr)
		}
		log.Printf("waiting for database: %v", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// Migrate applies embedded migrations that have not been applied yet.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var applied bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		// Serialize concurrent starters.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7316001)`); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&applied); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if !applied {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				tx.Rollback(ctx)
				return fmt.Errorf("migration %s: %w", name, err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES ($1)`, name); err != nil {
				tx.Rollback(ctx)
				return err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// SeedUser is one demo account.
type SeedUser struct {
	Username, Password, Role, DisplayName string
}

// DemoUsers are the accounts seeded on start (SPEC §4).
var DemoUsers = []SeedUser{
	{"prod", "prod", "A1", "Оператор производства"},
	{"retail", "retail", "A2", "Оператор розницы"},
	{"qc", "qc", "A3", "Контролёр качества"},
	{"auditor", "auditor", "A4", "Аудитор"},
	{"admin", "admin", "admin", "Администратор"},
}

// SeedUsers inserts demo users that do not exist yet.
func SeedUsers(ctx context.Context, pool *pgxpool.Pool, cost int) error {
	for _, u := range DemoUsers {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username=$1)`, u.Username).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(u.Password), cost)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, `INSERT INTO users(username, password_hash, role, display_name)
			VALUES ($1,$2,$3,$4) ON CONFLICT (username) DO NOTHING`,
			u.Username, string(hash), u.Role, u.DisplayName); err != nil {
			return err
		}
	}
	return nil
}

// Init runs migrations and seeding.
func Init(ctx context.Context, pool *pgxpool.Pool, bcryptCost int) error {
	if err := Migrate(ctx, pool); err != nil {
		return err
	}
	return SeedUsers(ctx, pool, bcryptCost)
}
