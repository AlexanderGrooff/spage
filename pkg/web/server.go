package web

import (
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/AlexanderGrooff/spage/pkg/database"
	"github.com/AlexanderGrooff/spage/pkg/generator"
)

type Server struct {
	db        *database.DB
	generator *generator.Generator
}

func NewServer(db *database.DB) *Server {
	return &Server{
		db:        db,
		generator: generator.NewGenerator(db),
	}
}

func (s *Server) Start() {
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
	router.POST("/generate", s.handleGenerate)

	// Add new endpoint to list binaries
	router.GET("/binaries", s.handleListBinaries)

	// Run the server
	router.Run(":8080")
}

func (s *Server) handleGenerate(c *gin.Context) {
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
	binaryPath, err := s.generator.GenerateBinary(tmpFile.Name(), "spage")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to generate tasks: %s", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tasks generated successfully", "binaryPath": binaryPath})
}

func (s *Server) handleListBinaries(c *gin.Context) {
	binaries, err := s.db.ListBinaries()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to list binaries: %s", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"binaries": binaries})
} 