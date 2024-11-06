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

	// API routes
	api := router.Group("/api")
	{
		api.GET("/binaries", s.handleListBinaries)
		api.GET("/binaries/grouped", s.handleListBinariesGrouped)
		api.POST("/generate", s.handleGenerate)
		api.GET("/download/:filename", s.handleDownload)
	}

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

func (s *Server) handleDownload(c *gin.Context) {
	filename := c.Param("filename")
	
	// Verify the file exists in the database
	exists, err := s.db.BinaryExists(filename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to check binary: %s", err)})
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Binary not found"})
		return
	}

	// Set Content-Disposition header for download
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Header("Content-Type", "application/octet-stream")

	// Serve the file
	c.File(filename)
}

// New handler for grouped binaries
func (s *Server) handleListBinariesGrouped(c *gin.Context) {
	binaryGroups, err := s.db.ListBinariesGrouped()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to list binaries: %s", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"binaryGroups": binaryGroups})
} 