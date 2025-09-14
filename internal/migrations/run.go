package migrations

import (
	"embed"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	_ "github.com/golang-migrate/migrate/v4/database/postgres"
)

//go:embed *.sql
var fs embed.FS

// Run applies all up migrations embedded in this package.
func Run() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	// iofs driver from embedded files
	d, err := iofs.New(fs, ".")
	if err != nil {
		log.Fatalf("iofs: %v", err)
	}

	// database driver
	m, err := migrate.NewWithSourceInstance("iofs", d, dsn)
	if err != nil {
		log.Fatalf("migrate new: %v", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("migrate up: %v", err)
	}
}
