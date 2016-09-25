package pgmigration

import (
	"database/sql"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// database interface needs to be inmplemented to migrate a new type of database
type database interface {
	createMigrationsTable() error
	hasMigrated(filename string) (bool, error)
	migrate(filename string, migration string) error
}

// postgresDatabase migrates Postgresql databases
type postgresDatabase struct {
	database *sql.DB
}

// CreateMigrationsTable create the table to keep track of versions of migration
func (postgres *postgresDatabase) createMigrationsTable() error {
	_, err := postgres.database.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			id serial,
			filename text NOT NULL,
			created_at timestamp with time zone NOT NULL,
			CONSTRAINT "PK_migrations_id" PRIMARY KEY (id)
	);`)
	if err != nil {
		return err
	}
	_, err = postgres.database.Exec("create unique index idx_migrations_name on migrations(filename)")
	if err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return err
		}
	}
	return nil
}

// HasMigrated check for migration
func (postgres *postgresDatabase) hasMigrated(filename string) (bool, error) {
	var count int
	err := postgres.database.QueryRow("select count(1) from migrations where filename = $1", filename).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Migrate perform exact migration
func (postgres *postgresDatabase) migrate(filename string, migration string) error {
	_, err := postgres.database.Exec(migration)
	if err != nil {
		return err
	}
	_, err = postgres.database.Exec("insert into migrations(filename, created_at) values($1, current_timestamp)", filename)
	return err
}

// newPostgresDatabase created a Postgresql connection
func newPostgresDatabase(db *sql.DB) *postgresDatabase {
	return &postgresDatabase{database: db}
}

// Run PostgreSQL migrations
func Run(db *sql.DB, migrationsFolder string) error {
	postgres := newPostgresDatabase(db)
	return applyMigrations(postgres, migrationsFolder)
}

// applyMigrations Run applies migrations from migrationsFolder to database.
func applyMigrations(database database, migrationsFolder string) error {
	// Initialize migrations table, if it does not exist yet
	if err := database.createMigrationsTable(); err != nil {
		return err
	}

	// Scan migration file names in migrations folder
	d, err := os.Open(migrationsFolder)
	if err != nil {
		return err
	}
	dir, err := d.Readdir(-1)
	if err != nil {
		return err
	}

	// Run migrations
	var sqlFiles []string
	for _, f := range dir {
		ext := filepath.Ext(f.Name())
		if ".sql" == ext {
			sqlFiles = append(sqlFiles, f.Name())
		}
	}
	sort.Strings(sqlFiles)
	for _, filename := range sqlFiles {
		// if exists in migrations table, leave it
		// else execute sql
		migrated, err := database.hasMigrated(filename)
		if err != nil {
			return err
		}
		fullpath := filepath.Join(migrationsFolder, filename)
		if migrated {
			log.Println("Already migrated", fullpath)
			continue
		}
		b, err := ioutil.ReadFile(fullpath)
		if err != nil {
			return err
		}
		migration := string(b)
		if len(migration) == 0 {
			log.Println("Skipping empty file", fullpath)
			continue // empty file
		}
		err = database.migrate(filename, migration)
		if err != nil {
			return err
		}
		log.Println("Migrated", fullpath)
	}

	return nil
}