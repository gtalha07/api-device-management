package database

import (
	"os"
	"testing"
)

func TestConnectUnreachable(t *testing.T) {
	// Port 1 has nothing listening, so the ping must fail fast.
	_, err := Connect(t.Context(), "postgres://u:p@127.0.0.1:1/db?connect_timeout=1")
	if err == nil {
		t.Fatal("Connect to an unreachable database: want error, got nil")
	}
}

func TestConnect(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres integration test")
	}

	pool, err := Connect(t.Context(), url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	pool.Close()
}
