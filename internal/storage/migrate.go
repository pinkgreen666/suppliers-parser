package storage

import (
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/iofs"
)

var migrationsFS embed.FS

func Migrate(db *sql.DB) error {
	driver, err := sqlite.WithInstance(db, &sqlite.Config{})
	if err != nil {
		return fmt.Errorf("sqlite driver: %w", err)
	}

	src, err := iofs
}
