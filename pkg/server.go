package pkg

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server represents the HTTP server
type Server struct {
	router *gin.Engine
	port   string
}

// NewServer creates a new HTTP server
func NewServer(port string) *Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	return &Server{
		router: router,
		port:   port,
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	// Metrics endpoint
	s.router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Health check endpoint
	s.router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	// API endpoints
	api := s.router.Group("/api/v1")
	{
		api.GET("/tasks", s.listTasks)
		api.GET("/tasks/:id", s.getTask)
		api.POST("/tasks", s.createTask)
	}

	return s.router.Run(":" + s.port)
}

func (s *Server) listTasks(c *gin.Context) {
	// TODO: Implement task listing
	c.JSON(http.StatusOK, gin.H{
		"tasks": []string{},
	})
}

func (s *Server) getTask(c *gin.Context) {
	id := c.Param("id")
	// TODO: Implement task retrieval
	c.JSON(http.StatusOK, gin.H{
		"task_id": id,
	})
}

func (s *Server) createTask(c *gin.Context) {
	// TODO: Implement task creation
	c.JSON(http.StatusCreated, gin.H{
		"status": "created",
	})
}
