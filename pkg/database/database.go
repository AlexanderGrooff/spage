package database

import (
	"fmt"
	"sort"
	"time"

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

type BinaryInfo struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	Path      string    `json:"path"`
}

type BinaryGroup struct {
	Name     string       `json:"name"`
	Versions []BinaryInfo `json:"versions"`
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

func (db *DB) GetBinaryVersions(name string) ([]Binary, error) {
	var binaries []Binary
	err := db.Where("name = ?", name).Order("version desc").Find(&binaries).Error
	return binaries, err
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
	var binaries []Binary
	err := db.Order("name, version desc").Find(&binaries).Error
	if err != nil {
		return nil, err
	}

	// Group binaries by name
	groupMap := make(map[string][]BinaryInfo)
	for _, bin := range binaries {
		info := BinaryInfo{
			Name:      bin.Name,
			Version:   fmt.Sprintf("v%d", bin.Version),
			CreatedAt: bin.CreatedAt,
			Path:      bin.Path,
		}
		groupMap[bin.Name] = append(groupMap[bin.Name], info)
	}

	// Convert map to slice of BinaryGroup
	var groups []BinaryGroup
	for name, versions := range groupMap {
		groups = append(groups, BinaryGroup{
			Name:     name,
			Versions: versions,
		})
	}

	// Sort groups by name
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})

	return groups, nil
}
