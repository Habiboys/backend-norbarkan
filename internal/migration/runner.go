package migration

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"backend-nobarkan/internal/config"
	_ "github.com/go-sql-driver/mysql"
)

type Direction string

const (
	DirectionUp   Direction = "up"
	DirectionDown Direction = "down"
)

type Runner struct {
	db            *sql.DB
	migrationsDir string
}

func NewRunner(cfg config.DatabaseConfig, migrationsDir string) (*Runner, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&multiStatements=true&loc=Local", cfg.User, cfg.Pass, cfg.Host, cfg.Port, cfg.Name)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return &Runner{db: db, migrationsDir: migrationsDir}, nil
}

func (r *Runner) Close() error {
	return r.db.Close()
}

func (r *Runner) Up() error {
	if err := r.ensureSchemaTable(); err != nil {
		return err
	}

	files, err := r.files(DirectionUp)
	if err != nil {
		return err
	}

	applied, err := r.appliedVersions()
	if err != nil {
		return err
	}

	for _, file := range files {
		version := versionFromFilename(file)
		if applied[version] {
			continue
		}

		if err := r.applyFile(file); err != nil {
			return fmt.Errorf("apply migration %s: %w", filepath.Base(file), err)
		}

		if _, err := r.db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", version, time.Now()); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}

		fmt.Printf("applied %s\n", filepath.Base(file))
	}

	return nil
}

func (r *Runner) Down(steps int) error {
	if steps <= 0 {
		return errors.New("steps harus lebih dari 0")
	}

	if err := r.ensureSchemaTable(); err != nil {
		return err
	}

	versions, err := r.latestAppliedVersions(steps)
	if err != nil {
		return err
	}

	if len(versions) == 0 {
		fmt.Println("tidak ada migrasi yang perlu di-rollback")
		return nil
	}

	files, err := r.files(DirectionDown)
	if err != nil {
		return err
	}

	filesByVersion := make(map[string]string, len(files))
	for _, file := range files {
		filesByVersion[versionFromFilename(file)] = file
	}

	for _, version := range versions {
		file, ok := filesByVersion[version]
		if !ok {
			return fmt.Errorf("file down migration untuk versi %s tidak ditemukan", version)
		}

		if err := r.applyFile(file); err != nil {
			return fmt.Errorf("rollback migration %s: %w", filepath.Base(file), err)
		}

		if _, err := r.db.Exec("DELETE FROM schema_migrations WHERE version = ?", version); err != nil {
			return fmt.Errorf("hapus record migration %s: %w", version, err)
		}

		fmt.Printf("rolled back %s\n", filepath.Base(file))
	}

	return nil
}

func (r *Runner) ensureSchemaTable() error {
	_, err := r.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(255) PRIMARY KEY,
    applied_at DATETIME NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	return nil
}

func (r *Runner) files(direction Direction) ([]string, error) {
	pattern := filepath.Join(r.migrationsDir, fmt.Sprintf("*.%s.sql", direction))
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("read migration files: %w", err)
	}

	sort.Strings(files)
	if direction == DirectionDown {
		sort.Sort(sort.Reverse(sort.StringSlice(files)))
	}

	return files, nil
}

func (r *Runner) appliedVersions() (map[string]bool, error) {
	rows, err := r.db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	versions := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan migration version: %w", err)
		}
		versions[version] = true
	}

	return versions, rows.Err()
}

func (r *Runner) latestAppliedVersions(limit int) ([]string, error) {
	rows, err := r.db.Query("SELECT version FROM schema_migrations ORDER BY version DESC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("query latest migrations: %w", err)
	}
	defer rows.Close()

	versions := make([]string, 0, limit)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan latest migration: %w", err)
		}
		versions = append(versions, version)
	}

	return versions, rows.Err()
}

func (r *Runner) applyFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	if strings.TrimSpace(string(content)) == "" {
		return nil
	}

	_, err = r.db.Exec(string(content))
	return err
}

func versionFromFilename(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".up.sql")
	base = strings.TrimSuffix(base, ".down.sql")
	return base
}
