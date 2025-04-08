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
