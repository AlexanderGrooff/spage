package database

import (
	"gorm.io/gorm"
	"gorm.io/driver/sqlite"
)

type Binary struct {
	gorm.Model
	Path     string
	Playbook []byte
}

type DB struct {
	*gorm.DB
}

func NewDB(dataSourceName string) (*DB, error) {
	db, err := gorm.Open(sqlite.Open(dataSourceName), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// Auto Migrate the schema
	err = db.AutoMigrate(&Binary{})
	if err != nil {
		return nil, err
	}

	return &DB{db}, nil
}

func (db *DB) StoreBinary(path string, playbook []byte) error {
	binary := Binary{
		Path:     path,
		Playbook: playbook,
	}
	return db.Create(&binary).Error
}

func (db *DB) ListBinaries() ([]Binary, error) {
	var binaries []Binary
	err := db.Order("created_at desc").Find(&binaries).Error
	return binaries, err
} 