//go:build !daemon
// +build !daemon

package daemon

import (
	"time"
)

// ClientInterface defines the interface for daemon client operations
// This allows for graceful degradation when daemon functionality is not available
type ClientInterface interface {
	Connect() error
	Disconnect() error
	RegisterPlayStart(playbook, inventory string, variables map[string]string, executor string) error
	RegisterPlayCompletion() error
	RegisterPlayError(err error) error
	UpdateTaskResult(taskResult interface{}) error
	Close() error
	IsConnected() bool
}

// Config holds the client configuration
type Config struct {
	Endpoint string
	PlayID   string
	Timeout  time.Duration
}

// StubClient is a no-op implementation of ClientInterface
// Used when daemon functionality is not available (no build tag)
type StubClient struct{}

// NewClient creates a stub client when daemon build tag is not set
func NewClient(cfg *Config) (*StubClient, error) {
	return &StubClient{}, nil
}

// Connect is a no-op for stub client
func (s *StubClient) Connect() error {
	return nil
}

// Disconnect is a no-op for stub client
func (s *StubClient) Disconnect() error {
	return nil
}

// RegisterPlayStart is a no-op for stub client
func (s *StubClient) RegisterPlayStart(playbook, inventory string, variables map[string]string, executor string) error {
	return nil
}

// RegisterPlayCompletion is a no-op for stub client
func (s *StubClient) RegisterPlayCompletion() error {
	return nil
}

// RegisterPlayError is a no-op for stub client
func (s *StubClient) RegisterPlayError(err error) error {
	return nil
}

// UpdateTaskResult is a no-op for stub client
func (s *StubClient) UpdateTaskResult(taskResult interface{}) error {
	return nil
}

// Close is a no-op for stub client
func (s *StubClient) Close() error {
	return nil
}

// IsConnected always returns false for stub client
func (s *StubClient) IsConnected() bool {
	return false
}

// Type alias for backwards compatibility
type Client = StubClient
