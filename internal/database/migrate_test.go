package database

import (
	"os"
	"testing"

	"github.com/gtalha07/api-device-management/migrations"
)

func TestMigrate(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres integration test")
	}

	pool, err := Connect(t.Context(), url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(pool.Close)

	// Twice: an up-to-date database must not be an error.
	for range 2 {
		if err := Migrate(pool, migrations.FS); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
	}

	var dirty bool
	if err := pool.QueryRow(t.Context(), "SELECT dirty FROM schema_migrations").Scan(&dirty); err != nil {
		t.Fatalf("read schema_migrations (pool must still work after Migrate): %v", err)
	}
	if dirty {
		t.Error("schema_migrations is dirty after Migrate")
	}
}
