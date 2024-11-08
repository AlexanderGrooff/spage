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

func CopyProjectFiles(srcDir string) (string, error) {
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
		"cmd.go",
		"main.go",
		"pkg",
	}

	for _, file := range files {
		if err := pkg.CopyPath(file, filepath.Join(tmpDir, file)); err != nil {
			return "", fmt.Errorf("failed to copy %s: %w", file, err)
		}
	}

	return tmpDir, nil
}

func (g *Generator) GenerateGraphFromPlaybook(playbookPath string, outputDir string) (string, error) {
	if outputDir == "" {
		var err error
		outputDir, err = CopyProjectFiles(playbookPath)
		if err != nil {
			return "", fmt.Errorf("failed to copy project files: %w", err)
		}

		// Copy playbook to temp directory
		if err := pkg.CopyPath(playbookPath, filepath.Join(outputDir, playbookPath)); err != nil {
			return "", fmt.Errorf("failed to copy playbook: %w", err)
		}
	}
	// Generate tasks
	cmd := exec.Command("go", "run", "generate_tasks.go", "-file", playbookPath)
	cmd.Dir = outputDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to generate tasks: %s: %w", output, err)
	}
	return filepath.Join(outputDir, "generated/tasks.go"), nil
}

func (g *Generator) BuildBinary(outputName string, defaultCmd string) (string, error) {
	tmpDir, err := CopyProjectFiles(".")
	if err != nil {
		return "", fmt.Errorf("failed to copy project files: %w", err)
	}
	// defer os.RemoveAll(tmpDir)

	// If defaultCmd is provided, create a custom main.go
	if defaultCmd != "" {
		mainContent := fmt.Sprintf(`package main

func main() {
	RootCmd.SetArgs([]string{"%s"})
	RootCmd.Execute()
}`, defaultCmd)

		if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainContent), 0644); err != nil {
			return "", fmt.Errorf("failed to write custom main.go: %w", err)
		}
	}

	// Build binary
	outputPath := filepath.Join(tmpDir, outputName)
	cmd := exec.Command("go", "build", "-o", outputPath)
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to build binary: %s: %w", output, err)
	}
	fmt.Printf("Built binary in %s\n", outputPath)

	return outputPath, nil
}

func (g *Generator) BuildBinaryFromGraphForHost(graph *pkg.Graph, outputName, inventoryFile, hostname string) (string, error) {
	// binary, err := g.db.GetBinary(binaryId)
	// if err != nil {
	// 	return "", fmt.Errorf("failed to get binary path: %w", err)
	// }

	// graph, err := pkg.NewGraphFromPlaybook(binary.Playbook)
	// if err != nil {
	// 	return "", fmt.Errorf("failed to parse playbook: %w", err)
	// }
	// graph, err := pkg.CompilePlaybookForHost(graph, inventoryFile, hostname)
	// if err != nil {
	// 	return "", fmt.Errorf("failed to compile graph for host: %w", err)
	// }

	// tmpDir, err := os.MkdirTemp("", "spage-build-*")
	// if err != nil {
	// 	return "", fmt.Errorf("failed to create temp dir: %w", err)
	// }
	// // defer os.RemoveAll(tmpDir)

	graphPath := filepath.Join(filepath.Dir(outputName), "generated/tasks.go")
	if err := graph.SaveToFile(graphPath); err != nil {
		return "", fmt.Errorf("failed to save compiled graph to playbook: %w", err)
	}

	binaryPath, err := g.BuildBinary(outputName, "run")
	if err != nil {
		return "", fmt.Errorf("failed to build binary: %w", err)
	}

	// Store binary information in database with versioning
	if err := g.db.StoreBinary(outputName, binaryPath, []byte(graph.ToCode())); err != nil {
		return "", fmt.Errorf("failed to store binary in database: %w", err)
	}

	return binaryPath, nil
}

// New helper methods for the generator
func (g *Generator) GetBinaryByName(name string) ([]database.Binary, error) {
	return g.db.GetBinaryByName(name)
}

func (g *Generator) GetBinaryVersion(name string, version int) (*database.Binary, error) {
	return g.db.GetBinaryVersion(name, version)
}

func (g *Generator) GetLatestBinary(name string) (*database.Binary, error) {
	return g.db.GetLatestBinary(name)
}
