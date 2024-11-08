package database

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Binary struct {
	gorm.Model
	Name     string
	Path     string
	Playbook []byte
	Version  int
}

type DB struct {
	*gorm.DB
}

type BinaryGroup struct {
	Name     string   `json:"name"`
	Versions []Binary `json:"versions"`
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

func (db *DB) StoreBinary(name string, path string, playbook []byte) error {
	var latestBinary Binary
	result := db.Where("name = ?", name).Order("version desc").First(&latestBinary)

	nextVersion := 1
	if result.Error == nil {
		nextVersion = latestBinary.Version + 1
	}

	binary := Binary{
		Name:     name,
		Path:     path,
		Playbook: playbook,
		Version:  nextVersion,
	}
	return db.Create(&binary).Error
}

func (db *DB) GetBinaryByName(name string) ([]Binary, error) {
	var binaries []Binary
	err := db.Where("name = ?", name).Order("version desc").Find(&binaries).Error
	return binaries, err
}

func (db *DB) GetBinary(id uint) (*Binary, error) {
	var binary Binary
	err := db.Where("id = ?", id).First(&binary).Error
	return &binary, err
}

func (db *DB) GetBinaryPathById(id uint) (string, error) {
	var binary Binary
	err := db.Where("id = ?", id).First(&binary).Error
	return binary.Path, err
}

func (db *DB) GetBinaryVersion(name string, version int) (*Binary, error) {
	var binary Binary
	err := db.Where("name = ? AND version = ?", name, version).First(&binary).Error
	return &binary, err
}

func (db *DB) GetLatestBinary(name string) (*Binary, error) {
	var binary Binary
	err := db.Where("name = ?", name).Order("version desc").First(&binary).Error
	return &binary, err
}

func (db *DB) ListBinaries() ([]Binary, error) {
	var binaries []Binary
	err := db.Order("created_at desc").Find(&binaries).Error
	return binaries, err
}

func (db *DB) BinaryExists(filename string) (bool, error) {
	var count int64
	err := db.Model(&Binary{}).Where("path = ?", filename).Count(&count).Error
	return count > 0, err
}

func (db *DB) ListBinariesGrouped() ([]BinaryGroup, error) {
	// First get distinct binary names
	var names []string
	if err := db.Model(&Binary{}).Distinct("name").Order("name").Pluck("name", &names).Error; err != nil {
		return nil, err
	}

	// For each name, get all versions
	groups := make([]BinaryGroup, len(names))
	for i, name := range names {
		var versions []Binary
		if err := db.Where("name = ?", name).Order("version desc").Find(&versions).Error; err != nil {
			return nil, err
		}
		groups[i] = BinaryGroup{
			Name:     name,
			Versions: versions,
		}
	}

	return groups, nil
}
