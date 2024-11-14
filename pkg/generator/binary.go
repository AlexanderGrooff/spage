package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/AlexanderGrooff/spage/pkg"
)

func CopyProjectFiles(srcDir string) (string, error) {
	// Create a temporary directory for building
	tmpDir, err := os.MkdirTemp("", "spage-build-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Use source files from /usr/local/src/spage
	spageSrcDir := "/usr/local/src/spage"

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
		srcPath := filepath.Join(spageSrcDir, file)
		dstPath := filepath.Join(tmpDir, file)
		if err := pkg.CopyPath(srcPath, dstPath); err != nil {
			return "", fmt.Errorf("failed to copy %s: %w", file, err)
		}
	}

	return tmpDir, nil
}

func GenerateGraphFromPlaybook(playbookPath string, outputFile string) (string, error) {
	// if outputFile != "generated/tasks.go" {
	// 	var err error
	// 	outputDir, err = CopyProjectFiles(playbookPath)
	// 	if err != nil {
	// 		return "", fmt.Errorf("failed to copy project files: %w", err)
	// 	}

	// 	// Copy playbook to temp directory
	// 	if err := pkg.CopyPath(playbookPath, filepath.Join(outputDir, playbookPath)); err != nil {
	// 		return "", fmt.Errorf("failed to copy playbook: %w", err)
	// 	}
	// }

	graph, err := pkg.NewGraphFromFile(playbookPath)
	if err != nil {
		return "", fmt.Errorf("failed to generate graph: %w", err)
	}

	err = graph.SaveToFile(outputFile)
	if err != nil {
		return "", fmt.Errorf("failed to save graph to file: %w", err)
	}

	return outputFile, nil
}

func BuildBinary(outputName string, defaultCmd string) (string, error) {
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

	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputName), 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// Build binary
	cmd := exec.Command("go", "build", "-o", outputName)
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to build binary: %s: %w", output, err)
	}

	return outputName, nil
}

func BuildBinaryFromGraphForHost(graph *pkg.Graph, outputName, inventoryFile, hostname string) (string, error) {
	graphPath := filepath.Join(filepath.Dir(outputName), "generated/tasks.go")
	if err := graph.SaveToFile(graphPath); err != nil {
		return "", fmt.Errorf("failed to save compiled graph to playbook: %w", err)
	}

	binaryPath, err := BuildBinary(outputName, "run")
	if err != nil {
		return "", fmt.Errorf("failed to build binary: %w", err)
	}

	return binaryPath, nil
}
