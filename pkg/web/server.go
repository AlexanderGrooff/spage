package web

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/database"
	"github.com/AlexanderGrooff/spage/pkg/generator"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"     // swagger embed files
	ginSwagger "github.com/swaggo/gin-swagger" // gin-swagger middleware

	docs "github.com/AlexanderGrooff/spage/docs" // This will be auto-generated
	_ "gorm.io/driver/sqlite"
	_ "gorm.io/gorm"
)

// BinaryResponse represents the API response model for a binary
// @Description Binary file information
type BinaryResponse struct {
	ID        uint      `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Path      string    `json:"path"`
}

// ErrorResponse represents the API error response model
// @Description Error response from the API
type ErrorResponse struct {
	Error string `json:"error"`
}

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

// @title           Spage API
// @version         1.0
// @description     API for generating and managing binary files from playbooks
// @BasePath  /api
func (s *Server) Start() {
	// Initialize Gin router
	router := gin.Default()

	// API routes
	api := router.Group("/api/v1")
	{
		api.GET("/binaries", s.handleListBinaries)
		api.GET("/binaries/grouped", s.handleListBinariesGrouped)
		api.POST("/generate", s.handleGenerate)
		api.GET("/download/:filename", s.handleDownload)
	}

	// Swagger documentation
	docs.SwaggerInfo.BasePath = "/api/v1"
	router.GET("/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Run the server
	router.Run(":8080")
}

// @Summary     List all binaries
// @Description Get a list of all generated binaries
// @Tags        binaries
// @Produce     json
// @Success     200 {object} []BinaryResponse
// @Failure     500 {object} ErrorResponse
// @Router      /binaries [get]
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

// @Summary     List all binaries
// @Description Get a list of all generated binaries
// @Tags        binaries
// @Produce     json
// @Success     200 {object} []BinaryResponse
// @Failure     500 {object} ErrorResponse
// @Router      /binaries [get]
func (s *Server) handleListBinaries(c *gin.Context) {
	binaries, err := s.db.ListBinaries()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to list binaries: %s", err)})
		return
	}

	// Convert database.Binary to BinaryResponse
	responses := make([]BinaryResponse, len(binaries))
	for i, bin := range binaries {
		responses[i] = BinaryResponse{
			ID:        bin.ID,
			CreatedAt: bin.CreatedAt,
			UpdatedAt: bin.UpdatedAt,
			Name:      bin.Name,
			Version:   fmt.Sprintf("v%d", bin.Version),
			Path:      bin.Path,
		}
	}

	c.JSON(http.StatusOK, gin.H{"binaries": responses})
}

// @Summary     Download binary
// @Description Download a generated binary file
// @Tags        binaries
// @Produce     octet-stream
// @Param       filename path string true "Binary filename"
// @Success     200 {file} binary
// @Failure     404 {object} ErrorResponse
// @Failure     500 {object} ErrorResponse
// @Router      /download/{filename} [get]
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

// @Summary     List grouped binaries
// @Description Get a list of binaries grouped by name
// @Tags        binaries
// @Produce     json
// @Success     200 {object} map[string][]database.BinaryGroup
// @Failure     500 {object} ErrorResponse
// @Router      /binaries/grouped [get]
func (s *Server) handleListBinariesGrouped(c *gin.Context) {
	binaryGroups, err := s.db.ListBinariesGrouped()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to list binaries: %s", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"binaryGroups": binaryGroups})
}
