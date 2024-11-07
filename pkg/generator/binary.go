package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/database"
)

type Generator struct {
	db *database.DB
}

func NewGenerator(db *database.DB) *Generator {
	return &Generator{
		db: db,
	}
}

func (g *Generator) GenerateBinary(playbookPath, outputName string) (string, error) {
	// Check if output name is provided
	if outputName == "" {
		return "", fmt.Errorf("output name is required")
	}

	// Create a temporary directory for building
	tmpDir, err := os.MkdirTemp("", "spage-build-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	// defer os.RemoveAll(tmpDir)

	// Copy necessary files to temp directory
	files := []string{
		"docs",
		"generate_tasks.go",
		"generated",
		"go.mod",
		"go.sum",
		"main.go",
		"pkg",
	}

	for _, file := range files {
		if err := pkg.CopyPath(file, filepath.Join(tmpDir, file)); err != nil {
			return "", fmt.Errorf("failed to copy %s: %w", file, err)
		}
	}

	// Copy playbook to temp directory
	if err := pkg.CopyPath(playbookPath, filepath.Join(tmpDir, "playbook.yaml")); err != nil {
		return "", fmt.Errorf("failed to copy playbook: %w", err)
	}

	// Generate tasks
	cmd := exec.Command("go", "generate")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to generate tasks: %s: %w", output, err)
	}

	// Build binary
	outputPath := filepath.Join(tmpDir, outputName)
	cmd = exec.Command("go", "build", "-o", outputPath)
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to build binary: %s: %w", output, err)
	}
	fmt.Printf("Built binary in %s\n", outputPath)

	// Read the playbook content
	playbookContent, err := os.ReadFile(playbookPath)
	if err != nil {
		return "", fmt.Errorf("failed to read playbook: %w", err)
	}

	// Store binary information in database with versioning
	if err := g.db.StoreBinary(outputName, outputPath, playbookContent); err != nil {
		return "", fmt.Errorf("failed to store binary in database: %w", err)
	}

	return outputPath, nil
}

// New helper methods for the generator
func (g *Generator) GetBinaryVersions(name string) ([]database.Binary, error) {
	return g.db.GetBinaryVersions(name)
}

func (g *Generator) GetBinaryVersion(name string, version int) (*database.Binary, error) {
	return g.db.GetBinaryVersion(name, version)
}

func (g *Generator) GetLatestBinary(name string) (*database.Binary, error) {
	return g.db.GetLatestBinary(name)
}
