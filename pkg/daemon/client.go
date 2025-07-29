package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AlexanderGrooff/spage-protobuf/spage/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Client represents a gRPC client for communicating with the Spage daemon
type Client struct {
	conn   *grpc.ClientConn
	client core.SpageExecutionClient

	// Configuration
	endpoint string
	taskID   string
	timeout  time.Duration

	// Connection management
	mu        sync.RWMutex
	connected bool

	// Progress streaming
	progressStream core.SpageExecution_StreamProgressClient
	streamMu       sync.RWMutex

	// Context for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

// Config holds the client configuration
type Config struct {
	Endpoint string
	TaskID   string
	Timeout  time.Duration
}

// NewClient creates a new gRPC client for daemon communication
func NewClient(cfg *Config) (*Client, error) {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "localhost:9091"
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	client := &Client{
		endpoint: cfg.Endpoint,
		taskID:   cfg.TaskID,
		timeout:  cfg.Timeout,
		ctx:      ctx,
		cancel:   cancel,
	}

	return client, nil
}

// Connect establishes a connection to the daemon
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	// Create connection with timeout
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, c.endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		// Check if it's a connection error
		if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
			return fmt.Errorf("daemon not available at %s: %w", c.endpoint, err)
		}
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}

	c.conn = conn
	c.client = core.NewSpageExecutionClient(conn)
	c.connected = true

	return nil
}

// Disconnect closes the connection to the daemon
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	// Close progress stream
	c.streamMu.Lock()
	if c.progressStream != nil {
		c.progressStream.CloseSend()
		c.progressStream = nil
	}
	c.streamMu.Unlock()

	// Close connection
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			return fmt.Errorf("failed to close connection: %w", err)
		}
		c.conn = nil
	}

	c.connected = false
	return nil
}

// RegisterTask registers the current task with the daemon
func (c *Client) RegisterTask(playbook, inventory string, variables map[string]string, engine string) error {
	if err := c.ensureConnected(); err != nil {
		// Check if it's a connection error
		if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
			return fmt.Errorf("daemon not available: %w", err)
		}
		return err
	}

	req := &core.RegisterTaskRequest{
		TaskId:    c.taskID,
		Playbook:  playbook,
		Inventory: inventory,
		Variables: variables,
		Engine:    engine,
		Metadata: map[string]string{
			"playbook":  playbook,
			"inventory": inventory,
			"engine":    engine,
		},
	}

	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	resp, err := c.client.RegisterTask(ctx, req)
	if err != nil {
		// Check if it's a connection error
		if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
			return fmt.Errorf("daemon not available: %w", err)
		}
		return fmt.Errorf("failed to register task: %w", err)
	}

	if !resp.Success {
		if resp.Error != nil {
			return fmt.Errorf("task registration failed: %s", resp.Error.Message)
		}
		return fmt.Errorf("task registration failed")
	}

	return nil
}

// UpdateProgress sends a progress update to the daemon
func (c *Client) UpdateProgress(progress float64, message string, metadata map[string]string) error {
	if c == nil {
		return nil // Return nil instead of error to avoid breaking task execution
	}

	// Check if we're connected first
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if !connected {
		// Try to connect, but don't fail if it doesn't work
		if err := c.Connect(); err != nil {
			// Silently ignore connection failures - daemon might not be running
			return nil
		}
	}

	// Ensure progress stream is established
	if err := c.ensureProgressStream(); err != nil {
		// Silently ignore stream establishment failures - daemon might not be running
		return nil
	}

	update := &core.ProgressUpdate{
		TaskId:    c.taskID,
		Progress:  progress,
		Message:   message,
		Metadata:  metadata,
		Timestamp: timestamppb.Now(),
	}

	c.streamMu.RLock()
	defer c.streamMu.RUnlock()

	if c.progressStream != nil {
		// Try to send the update with retry logic
		var err error
		for retries := 0; retries < 3; retries++ {
			err = c.progressStream.Send(update)
			if err == nil {
				break
			}

			// If it's a connection error, try to reconnect
			if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
				c.streamMu.RUnlock()
				c.streamMu.Lock()
				c.progressStream = nil // Reset stream to force reconnection
				c.streamMu.Unlock()
				c.streamMu.RLock()

				// Try to reestablish connection
				if reconnectErr := c.ensureProgressStream(); reconnectErr != nil {
					return nil // Don't fail the operation
				}
				continue
			}

			// For other errors, don't retry
			break
		}

		if err != nil {
			// Silently ignore send failures - daemon might not be running
			return nil
		}
	}

	return nil
}

// ReportError reports an error to the daemon
func (c *Client) ReportError(errorMsg string) error {
	// Silently ignore errors if daemon is not available
	return c.UpdateProgress(0.0, fmt.Sprintf("Error: %s", errorMsg), map[string]string{
		"error":  errorMsg,
		"status": "failed",
	})
}

// ReportCompletion reports task completion to the daemon
func (c *Client) ReportCompletion(result map[string]string) error {
	metadata := map[string]string{
		"status": "completed",
	}

	// Add result data to metadata
	for k, v := range result {
		metadata[k] = v
	}

	// Silently ignore errors if daemon is not available
	return c.UpdateProgress(100.0, "Task completed successfully", metadata)
}

// GetTaskStatus retrieves the current task status from the daemon
func (c *Client) GetTaskStatus() (*core.Task, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	req := &core.GetTaskStatusRequest{
		TaskId: c.taskID,
	}

	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	resp, err := c.client.GetTaskStatus(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get task status: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("task status error: %s", resp.Error.Message)
	}

	return resp.Task, nil
}

// HealthCheck performs a health check on the daemon
func (c *Client) HealthCheck() error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	_, err := c.client.HealthCheck(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("daemon health check failed: %w", err)
	}

	return nil
}

// GetMetrics retrieves metrics from the daemon
func (c *Client) GetMetrics() (*core.Metrics, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	metrics, err := c.client.GetMetrics(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}

	return metrics, nil
}

// ensureConnected ensures the client is connected to the daemon
func (c *Client) ensureConnected() error {
	if c == nil {
		return fmt.Errorf("daemon client is nil")
	}

	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if !connected {
		return c.Connect()
	}

	return nil
}

// ensureProgressStream ensures the progress stream is established
func (c *Client) ensureProgressStream() error {
	if c == nil {
		return fmt.Errorf("daemon client is nil")
	}

	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.streamMu.Lock()
	defer c.streamMu.Unlock()

	if c.progressStream != nil {
		return nil
	}

	// Create progress stream
	stream, err := c.client.StreamProgress(c.ctx)
	if err != nil {
		// Check if it's a connection error
		if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
			return fmt.Errorf("daemon not available: %w", err)
		}
		return fmt.Errorf("failed to create progress stream: %w", err)
	}

	c.progressStream = stream

	// Start receiving updates in background
	go c.receiveProgressUpdates()

	return nil
}

// receiveProgressUpdates receives progress updates from the daemon
func (c *Client) receiveProgressUpdates() {
	for {
		// Check if stream is still valid
		c.streamMu.RLock()
		stream := c.progressStream
		c.streamMu.RUnlock()

		if stream == nil {
			// Stream was closed or reset, exit gracefully
			return
		}

		update, err := stream.Recv()
		if err != nil {
			// Check if it's a graceful close
			if status.Code(err) == codes.Canceled {
				return
			}

			// For connection errors, exit gracefully without logging
			if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
				return
			}

			// For other errors, log and exit
			fmt.Printf("Error receiving progress update: %v\n", err)
			return
		}

		// Handle incoming progress updates (if needed)
		fmt.Printf("Received progress update: %s - %.1f%%\n", update.Message, update.Progress)
	}
}

// Close closes the client and cleans up resources
func (c *Client) Close() error {
	c.cancel()
	return c.Disconnect()
}

// IsConnected returns whether the client is connected to the daemon
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}
