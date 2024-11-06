package database

import (
	"spage/pkg/web"
)

type DB struct {
	// Add any necessary fields here
}

func (db *DB) ListBinariesWithInfo() ([]web.BinaryInfo, error) {
	// Implementation will depend on your database structure
	// Should return slice of BinaryInfo with Name, Version, CreatedAt, and Path
} 