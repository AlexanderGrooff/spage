//go:generate go run generate_tasks.go -file playbook.yaml

package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"

	"github.com/AlexanderGrooff/spage/generated"
	"github.com/AlexanderGrooff/spage/pkg"
)

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

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Run the web server",
	Run: func(cmd *cobra.Command, args []string) {
		StartWebserver()
	},
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a binary from a playbook",
	Run: func(cmd *cobra.Command, args []string) {
		binaryPath, err := GenerateBinary(playbookFile, outputFile)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Printf("Generated binary in %s\n", binaryPath)
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
	rootCmd.AddCommand(webCmd)
}

func GenerateBinary(playbookPath, outputPath string) (string, error) {
	// Create a temporary directory for building
	tmpDir, err := os.MkdirTemp("", "spage-build-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
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
	cmd = exec.Command("go", "build", "-o", outputPath)
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to build binary: %s: %w", output, err)
	}
	fmt.Printf("Built binary in %s\n", filepath.Join(tmpDir, outputPath))

	return filepath.Join(tmpDir, outputPath), nil
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

func StartWebserver() {
	// Initialize Gin router
	router := gin.Default()

	// Load templates from the templates directory
	router.LoadHTMLGlob("web/templates/*")

	// Define homepage route
	router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"title": "Homepage",
		})
	})

	// Define generate endpoint
	router.POST("/generate", func(c *gin.Context) {
		var content []byte
		var err error

		contentType := c.GetHeader("Content-Type")

		// Handle different content types
		switch contentType {
		case "application/json":
			var payload struct {
				Content string `json:"content"`
			}
			if err := c.BindJSON(&payload); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to parse JSON: %s", err)})
				return
			}
			content = []byte(payload.Content)
		case "application/yaml", "text/yaml":
			content, err = c.GetRawData()
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to read request body: %s", err)})
				return
			}
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported Content-Type. Use application/json or application/yaml"})
			return
		}

		// Create temporary file for playbook
		tmpFile, err := os.CreateTemp("", "playbook-*.yaml")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to create temporary file: %s", err)})
			return
		}
		defer os.Remove(tmpFile.Name())

		// Write playbook content to file
		if _, err := tmpFile.Write(content); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to write playbook: %s", err)})
			return
		}

		// Run go generate with the playbook file
		binaryPath, err := GenerateBinary(tmpFile.Name(), "spage")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to generate tasks: %s", err)})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Tasks generated successfully", "binaryPath": binaryPath})
	})

	// Run the server
	router.Run(":8080")
}
