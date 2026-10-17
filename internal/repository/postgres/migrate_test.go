package postgres

import (
	"bytes"
	"io/fs"
	"testing"
)

func TestEmbeddedMigrations(t *testing.T) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one migration")
	}
	for _, entry := range entries {
		body, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if len(bytes.TrimSpace(body)) == 0 {
			t.Fatalf("%s is empty", entry.Name())
		}
	}
}
