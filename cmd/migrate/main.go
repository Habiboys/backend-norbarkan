package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"backend-nobarkan/internal/config"
	_ "github.com/go-sql-driver/mysql"
)

type appliedMigration struct {
	Checksum string
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := sql.Open("mysql", migrationDSN(cfg.DB))
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("connect db: %v", err)
	}

	unlock, err := acquireLock(ctx, db)
	if err != nil {
		log.Fatalf("acquire migration lock: %v", err)
	}
	defer unlock()

	hadMigrationTable, err := tableExists(ctx, db, "schema_migrations")
	if err != nil {
		log.Fatalf("check migration table: %v", err)
	}
	if err := ensureMigrationTable(ctx, db); err != nil {
		log.Fatalf("ensure migration table: %v", err)
	}

	files, err := migrationFiles()
	if err != nil {
		log.Fatalf("read migrations: %v", err)
	}
	if len(files) == 0 {
		log.Println("no migration files found")
		return
	}

	if !hadMigrationTable {
		hasSchema, err := hasExistingSchema(ctx, db)
		if err != nil {
			log.Fatalf("check existing schema: %v", err)
		}
		if hasSchema {
			baselineMigrations(ctx, db, files)
			log.Println("existing schema detected; current migrations baselined without rerun")
			return
		}
	}

	applied, err := loadAppliedMigrations(ctx, db)
	if err != nil {
		log.Fatalf("load applied migrations: %v", err)
	}

	for _, path := range files {
		name := filepath.Base(path)
		content, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("read migration %s: %v", name, err)
		}
		checksum := sha256Hex(content)

		if current, ok := applied[name]; ok {
			if current.Checksum != checksum {
				log.Fatalf("migration %s already applied with different checksum", name)
			}
			log.Printf("skip %s", name)
			continue
		}

		log.Printf("apply %s", name)
		if _, err := db.ExecContext(ctx, string(content)); err != nil {
			log.Fatalf("apply migration %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO schema_migrations (version, checksum, applied_at)
			VALUES (?, ?, UTC_TIMESTAMP())
		`, name, checksum); err != nil {
			log.Fatalf("record migration %s: %v", name, err)
		}
	}

	log.Println("migrations complete")
}

func migrationDSN(cfg config.DatabaseConfig) string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local&multiStatements=true", cfg.User, cfg.Pass, cfg.Host, cfg.Port, cfg.Name)
}

func acquireLock(ctx context.Context, db *sql.DB) (func(), error) {
	var got int
	if err := db.QueryRowContext(ctx, "SELECT GET_LOCK('nobarkan_schema_migrations', 60)").Scan(&got); err != nil {
		return nil, err
	}
	if got != 1 {
		return nil, fmt.Errorf("migration lock timeout")
	}
	return func() {
		_, _ = db.ExecContext(context.Background(), "SELECT RELEASE_LOCK('nobarkan_schema_migrations')")
	}, nil
}

func ensureMigrationTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			checksum CHAR(64) NOT NULL,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	return err
}

func tableExists(ctx context.Context, db *sql.DB, tableName string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_name = ?
	`, tableName).Scan(&count)
	return count > 0, err
}

func hasExistingSchema(ctx context.Context, db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_name <> 'schema_migrations'
	`).Scan(&count)
	return count > 0, err
}

func baselineMigrations(ctx context.Context, db *sql.DB, files []string) {
	for _, path := range files {
		name := filepath.Base(path)
		content, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO schema_migrations (version, checksum, applied_at)
			VALUES (?, ?, UTC_TIMESTAMP())
		`, name, sha256Hex(content)); err != nil {
			log.Fatalf("baseline migration %s: %v", name, err)
		}
		log.Printf("baseline %s", name)
	}
}

func loadAppliedMigrations(ctx context.Context, db *sql.DB) (map[string]appliedMigration, error) {
	rows, err := db.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]appliedMigration)
	for rows.Next() {
		var version string
		var migration appliedMigration
		if err := rows.Scan(&version, &migration.Checksum); err != nil {
			return nil, err
		}
		result[version] = migration
	}
	return result, rows.Err()
}

func migrationFiles() ([]string, error) {
	matches, err := filepath.Glob(filepath.Join("migrations", "*.up.sql"))
	if err != nil {
		return nil, err
	}
	sort.Slice(matches, func(i, j int) bool {
		return strings.Compare(filepath.Base(matches[i]), filepath.Base(matches[j])) < 0
	})
	return matches, nil
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
