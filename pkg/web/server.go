package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/database"
	"github.com/AlexanderGrooff/spage/pkg/generator"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"     // swagger embed files
	ginSwagger "github.com/swaggo/gin-swagger" // gin-swagger middleware

	"github.com/AlexanderGrooff/spage/docs" // This will be auto-generated
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
	Playbook  string    `json:"playbook"`
}

func BinaryToResponse(b database.Binary) BinaryResponse {
	return BinaryResponse{
		ID:        b.ID,
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
		Name:      b.Name,
		Version:   fmt.Sprintf("v%d", b.Version),
		Path:      b.Path,
		Playbook:  string(b.Playbook),
	}
}

type BinaryGroupResponse struct {
	Name     string           `json:"name"`
	Versions []BinaryResponse `json:"versions"`
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

	// Add CORS middleware
	config := cors.DefaultConfig()
	config.AllowOrigins = []string{"http://localhost:3000"} // Allow Next.js dev server
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"Origin", "Content-Type", "Accept"}
	router.Use(cors.New(config))

	// API routes
	api := router.Group("/api/v1")
	{
		api.GET("/binaries", s.handleListBinaries)
		api.GET("/binaries/grouped", s.handleListBinariesGrouped)
		api.GET("/binaries/:id/download", s.handleDownload)
		api.POST("/generate", s.handleGenerate)
		api.POST("/generate/host/:id/:hostname", s.handleGenerateForHost)
	}

	// Swagger documentation
	docs.SwaggerInfo.BasePath = "/api/v1"
	router.GET("/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	router.GET("/openapi.json", func(c *gin.Context) {
		doc := docs.SwaggerInfo.ReadDoc()
		var jsonDoc interface{}
		if err := json.Unmarshal([]byte(doc), &jsonDoc); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse OpenAPI document"})
			return
		}
		c.JSON(http.StatusOK, jsonDoc)
	})

	// Get port from environment variable or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Run the server
	router.Run(":" + port)
}

// @Summary     Generate binary from playbook
// @Description Generate a binary from a playbook
// @Tags        generate
// @Param       name body string true "Binary name"
// @Param       playbook body string true "Playbook content"
// @Produce     json
// @Success     200 {object} map[string]interface{}
// @Failure     500 {object} ErrorResponse
// @Router      /generate [post]
func (s *Server) handleGenerate(c *gin.Context) {
	var content []byte
	var err error
	var name string
	contentType := c.GetHeader("Content-Type")

	// Handle different content types
	switch contentType {
	case "application/json":
		var payload struct {
			Content string `json:"content"`
			Name    string `json:"name"`
		}
		if err := c.BindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to parse JSON: %s", err)})
			return
		}
		content = []byte(payload.Content)
		name = payload.Name
	case "application/yaml", "text/yaml", "text/plain", "":
		content, err = c.GetRawData()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to read request body: %s", err)})
			return
		}
		name = c.Query("name")
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Name parameter is required"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Unsupported Content-Type '%s'. Use application/json, application/yaml, or text/plain", contentType)})
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
	graph, err := s.generator.GenerateGraphFromPlaybook(tmpFile.Name(), "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to generate tasks: %s", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Graph generated successfully", "graph": graph})
}

// @Summary     Generate binary for host
// @Description Generate a binary for a specific host
// @Tags        generate
// @Param       id path int true "Binary ID"
// @Param       hostname path string true "Host name"
// @Produce     json
// @Success     200 {object} map[string]interface{}
// @Failure     500 {object} ErrorResponse
// @Router      /generate/host/{id}/{hostname} [post]
func (s *Server) handleGenerateForHost(c *gin.Context) {
	// id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	// if err != nil {
	// 	c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid binary ID: %s", err)})
	// 	return
	// }

	// binary, err := s.db.GetBinary(uint(id))
	// if err != nil {
	// 	c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get binary: %s", err)})
	// 	return
	// }
	// graph, err := pkg.NewGraphFromPlaybook(binary.Playbook)
	// if err != nil {
	// 	c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to parse playbook: %s", err)})
	// 	return
	// }
	// binaryPath, err := s.generator.BuildBinaryFromGraphForHost(&graph, binary.Name, "inventory.yaml", c.Param("hostname"))
	// if err != nil {
	// 	c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to generate tasks: %s", err)})
	// 	return
	// }
	// c.JSON(http.StatusOK, gin.H{"message": "Tasks generated successfully", "binaryPath": binaryPath})
	c.JSON(http.StatusOK, gin.H{"message": "TBD"})
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
		responses[i] = BinaryToResponse(bin)
	}

	c.JSON(http.StatusOK, responses)
}

// @Summary     List grouped binaries
// @Description Get a list of binaries grouped by name
// @Tags        binaries
// @Produce     json
// @Success     200 {object} []BinaryGroupResponse
// @Failure     500 {object} ErrorResponse
// @Router      /binaries/grouped [get]
func (s *Server) handleListBinariesGrouped(c *gin.Context) {
	binaryGroups, err := s.db.ListBinariesGrouped()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to list binaries: %s", err)})
		return
	}

	responses := make([]BinaryGroupResponse, len(binaryGroups))
	for i, group := range binaryGroups {
		versions := make([]BinaryResponse, len(group.Versions))
		for j, version := range group.Versions {
			versions[j] = BinaryToResponse(version)
		}
		responses[i] = BinaryGroupResponse{
			Name:     group.Name,
			Versions: versions,
		}
	}

	c.JSON(http.StatusOK, responses)
}

// @Summary     Download specific binary version
// @Description Download a specific version of a binary file
// @Tags        binaries
// @Produce     octet-stream
// @Param       id path int true "Binary ID"
// @Success     200 {file} binary
// @Failure     404 {object} ErrorResponse
// @Failure     500 {object} ErrorResponse
// @Router      /binaries/{id}/download [get]
func (s *Server) handleDownload(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid binary ID: %s", err)})
		return
	}

	// Get the binary path from the database
	binaryPath, err := s.db.GetBinaryPathById(uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get binary: %s", err)})
		return
	}
	if binaryPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Binary version not found"})
		return
	}

	// Set Content-Disposition header for download
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", binaryPath))
	c.Header("Content-Type", "application/octet-stream")

	// Serve the file
	c.File(binaryPath)
}
