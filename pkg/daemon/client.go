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
	progressStream core.SpageExecution_StreamTaskProgressClient
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

// RegisterPlayStart registers the current play with the daemon
func (c *Client) RegisterPlayStart(playbook, inventory string, variables map[string]string, executor string) error {
	if err := c.ensureConnected(); err != nil {
		// Check if it's a connection error
		if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
			return fmt.Errorf("daemon not available: %w", err)
		}
		return err
	}

	req := &core.RegisterPlayRequest{
		PlayId:    c.taskID,
		Playbook:  playbook,
		Inventory: inventory,
		Variables: variables,
		Executor:  executor,
		Metadata: map[string]string{
			"playbook":  playbook,
			"inventory": inventory,
			"executor":  executor,
		},
	}

	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	resp, err := c.client.RegisterPlay(ctx, req)
	if err != nil {
		// Check if it's a connection error
		if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
			return fmt.Errorf("daemon not available: %w", err)
		}
		return fmt.Errorf("failed to register play: %w", err)
	}

	if !resp.Success {
		if resp.Error != nil {
			return fmt.Errorf("play registration failed: %s", resp.Error.Message)
		}
		return fmt.Errorf("play registration failed")
	}

	return nil
}

func (c *Client) RegisterPlayCompletion() error {
	req := &core.RegisterPlayCompletionRequest{
		PlayId: c.taskID,
	}

	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	resp, err := c.client.RegisterPlayCompletion(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to register play completion: %w", err)
	}

	if !resp.Success {
		if resp.Error != nil {
			return fmt.Errorf("play completion registration failed: %s", resp.Error.Message)
		}
		return fmt.Errorf("play completion registration failed")
	}

	return nil
}

// UpdateTaskResult sends a task result update to the daemon
func (c *Client) UpdateTaskResult(taskResult *core.TaskResult) error {
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

	// Ensure the TaskResult has the correct TaskId (should be the actual task name/ID)
	if taskResult.TaskId == "" {
		taskResult.TaskId = "unknown-task"
	}

	update := &core.TaskProgressUpdate{
		TaskId:    c.taskID, // Use the play ID for the TaskProgressUpdate.TaskId (this is actually the play ID)
		Result:    taskResult,
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

// UpdateTaskProgress sends a progress update to the daemon (simplified version)
func (c *Client) UpdateTaskProgress(progress float64, metadata map[string]string) error {
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

	// Determine task status from metadata if available
	var taskStatus core.TaskStatus
	var errorMsg string

	if statusStr, ok := metadata["status"]; ok {
		switch statusStr {
		case "completed":
			taskStatus = core.TaskStatus_TASK_STATUS_COMPLETED
		case "failed":
			taskStatus = core.TaskStatus_TASK_STATUS_FAILED
			if errMsg, ok := metadata["error"]; ok {
				errorMsg = errMsg
			}
		case "skipped":
			taskStatus = core.TaskStatus_TASK_STATUS_SKIPPED
		default:
			taskStatus = core.TaskStatus_TASK_STATUS_RUNNING
		}
	} else {
		// Fallback to progress-based logic for backward compatibility
		if progress >= 100.0 {
			taskStatus = core.TaskStatus_TASK_STATUS_COMPLETED
		} else if progress < 0.0 {
			taskStatus = core.TaskStatus_TASK_STATUS_FAILED
			if errMsg, ok := metadata["error"]; ok {
				errorMsg = errMsg
			}
		} else {
			taskStatus = core.TaskStatus_TASK_STATUS_RUNNING
		}
	}

	update := &core.TaskProgressUpdate{
		TaskId: c.taskID,
		Result: &core.TaskResult{
			TaskId: c.taskID,
			Status: taskStatus,
			Error:  errorMsg,
		},
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
	stream, err := c.client.StreamTaskProgress(c.ctx)
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
		if update.Result != nil {
			fmt.Printf("Received task result: %s - %s\n", update.TaskId, update.Result.Status)
		} else {
			fmt.Printf("Received progress update: %s\n", update.TaskId)
		}
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

// GetTaskID returns the task ID (which is also the play ID)
func (c *Client) GetTaskID() string {
	return c.taskID
}
