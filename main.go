//go:generate go run generate_tasks.go -file playbook.yaml

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/AlexanderGrooff/spage/generated"
	"github.com/AlexanderGrooff/spage/pkg"
)

// Task struct definition...

var (
	playbookFile  string
	outputFile    string
	inventoryFile string
)

var rootCmd = &cobra.Command{
	Use:   "spage",
	Short: "Simple Playbook AGEnt",
	Long:  `A lightweight configuration management tool that compiles your playbooks into a single binary.`,
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the playbook",
	Run: func(cmd *cobra.Command, args []string) {
		if err := pkg.Execute(generated.Graph, inventoryFile); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a binary from a playbook",
	Run: func(cmd *cobra.Command, args []string) {
		if err := generateBinary(playbookFile, outputFile); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
}

func init() {
	runCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file (required)")
	generateCmd.Flags().StringVarP(&playbookFile, "playbook", "p", "", "Playbook file (required)")
	generateCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output binary name (required)")

	generateCmd.MarkFlagRequired("playbook")
	generateCmd.MarkFlagRequired("output")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(generateCmd)
}

func generateBinary(playbookPath, outputPath string) error {
	// Create a temporary directory for building
	tmpDir, err := os.MkdirTemp("", "spage-build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	// defer os.RemoveAll(tmpDir)

	// Copy necessary files to temp directory
	files := []string{
		"go.mod",
		"go.sum",
		"main.go",
		"pkg",
		"generated",
		"generate_tasks.go",
	}

	for _, file := range files {
		if err := pkg.CopyPath(file, filepath.Join(tmpDir, file)); err != nil {
			return fmt.Errorf("failed to copy %s: %w", file, err)
		}
	}

	// Copy playbook to temp directory
	if err := pkg.CopyPath(playbookPath, filepath.Join(tmpDir, "playbook.yaml")); err != nil {
		return fmt.Errorf("failed to copy playbook: %w", err)
	}

	// Generate tasks
	cmd := exec.Command("go", "generate")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to generate tasks: %s: %w", output, err)
	}

	// Build binary
	cmd = exec.Command("go", "build", "-o", outputPath)
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build binary: %s: %w", output, err)
	}
	fmt.Printf("Built binary in %s\n", filepath.Join(tmpDir, outputPath))

	return nil
}

func main() {

	if err := Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}

func Execute() error {
	return rootCmd.Execute()
}
