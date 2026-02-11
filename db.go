package db

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/kelseyhightower/envconfig"
	_ "github.com/mattn/go-sqlite3"
)

var (
	writeDB     *sql.DB
	readDB      *sql.DB
	writeDBOnce sync.Once
	readDBOnce  sync.Once
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type config struct {
	Path          string `default:"sqlite.db" split_words:"true"`
	JournalMode   string `default:"WAL" split_words:"true"`
	Synchronous   string `default:"NORMAL" split_words:"true"`
	CacheSizeMB   int    `default:"250" split_words:"true"`
	ForeignKeys   bool   `default:"true" split_words:"true"`
	BusyTimeout   int    `default:"5000" split_words:"true"`
	TempStore     string `default:"memory" split_words:"true"`
	MaxWriteConns int    `default:"1" split_words:"true"`
	MaxReadConns  int    `default:"4" split_words:"true"`
}

func Init() {
	GetWriteDB()
	GetReadDB()
}

func GetWriteDB() *sql.DB {
	writeDBOnce.Do(func() {
		var cfg config
		envconfig.MustProcess("DB", &cfg)

		var err error
		writeDB, err = sql.Open("sqlite3", cfg.Path)
		if err != nil {
			panic(fmt.Errorf("failed to open write database: %w", err))
		}

		if err = configureDB(writeDB, cfg); err != nil {
			panic(fmt.Errorf("failed to configure write database: %w", err))
		}

		writeDB.SetMaxOpenConns(cfg.MaxWriteConns)

		if err = runMigrations(writeDB); err != nil {
			panic(fmt.Errorf("failed to run migrations: %w", err))
		}
	})

	return writeDB
}

func GetReadDB() *sql.DB {
	readDBOnce.Do(func() {
		var cfg config
		envconfig.MustProcess("DB", &cfg)

		var err error
		readDB, err = sql.Open("sqlite3", cfg.Path)
		if err != nil {
			panic(fmt.Errorf("failed to open read database: %w", err))
		}

		if err = configureDB(readDB, cfg); err != nil {
			panic(fmt.Errorf("failed to configure read database: %w", err))
		}

		maxReadConns := max(cfg.MaxReadConns, runtime.NumCPU())
		readDB.SetMaxOpenConns(maxReadConns)
	})

	return readDB
}

func configureDB(db *sql.DB, cfg config) error {
	// the negative cacheSize is correct
	// see https://www.sqlite.org/pragma.html#pragma_cache_size
	cacheSize := -1 * cfg.CacheSizeMB * 1024

	pragmas := map[string]string{
		"journal_mode": cfg.JournalMode,
		"synchronous":  cfg.Synchronous,
		"cache_size":   fmt.Sprintf("%d", cacheSize),
		"foreign_keys": fmt.Sprintf("%t", cfg.ForeignKeys),
		"busy_timeout": fmt.Sprintf("%d", cfg.BusyTimeout),
		"temp_store":   cfg.TempStore,
	}

	for name, value := range pragmas {
		query := fmt.Sprintf("PRAGMA %s = %s", name, value)
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to set PRAGMA %s: %w", name, err)
		}
	}

	return nil
}

func runMigrations(db *sql.DB) error {
	currentVersion, err := getCurrentVersion(db)
	if err != nil {
		return err
	}

	files, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}
		version, allowed := currentVersion.IsFileAllowed(file.Name())
		if !allowed {
			continue
		}

		query, err := migrationFiles.ReadFile("migrations/" + file.Name())
		if err != nil {
			return fmt.Errorf("reading %s: %w", file.Name(), err)
		}

		slog.Info("running migration", "migration", file.Name())
		if err := runMigrationInTransaction(db, file.Name(), string(query), version); err != nil {
			return err
		}

		currentVersion = version
	}

	return nil
}

func runMigrationInTransaction(db *sql.DB, fileName, query string, version dbVersion) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("starting transaction for %s: %w", fileName, err)
	}

	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				slog.Error("failed to rollback transaction", "error", rbErr)
			}
		}
	}()

	if _, err = tx.Exec(query); err != nil {
		return fmt.Errorf("executing %s: %w", fileName, err)
	}

	if err = saveNewVersionInTx(tx, version); err != nil {
		return fmt.Errorf("saving version for %s: %w", fileName, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction for %s: %w", fileName, err)
	}

	slog.Info("migrated DB to version", "version", version)
	return nil
}

type dbVersion int

func (v *dbVersion) IsFileAllowed(fileName string) (dbVersion, bool) {
	if v == nil {
		return 0, true
	}

	// assuming file name is always in format \d[4]_.*\.sql
	version, err := strconv.Atoi(fileName[:4])
	if err != nil {
		return 0, false
	}
	return dbVersion(version), int(*v) < version
}

func getCurrentVersion(db *sql.DB) (dbVersion, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER NOT NULL)`)
	if err != nil {
		return 0, fmt.Errorf("unable to create schema_migrations table: %w", err)
	}

	var currentVersion int
	row := db.QueryRow(`SELECT version FROM schema_migrations LIMIT 1`)
	err = row.Scan(&currentVersion)
	if err == sql.ErrNoRows {
		_, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, 0)
		if err != nil {
			return 0, fmt.Errorf("unable to set initial DB version: %w", err)
		}
		return 0, nil
	}

	return dbVersion(currentVersion), err
}

func saveNewVersionInTx(tx *sql.Tx, version dbVersion) error {
	_, err := tx.Exec(`UPDATE schema_migrations SET version = ?`, version)
	if err != nil {
		return fmt.Errorf("unable to save new DB version: %w", err)
	}
	return nil
}
