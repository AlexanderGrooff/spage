//go:build !daemon
// +build !daemon

package pkg

import (
	"time"

	"github.com/AlexanderGrooff/spage/pkg/daemon"
)

// Stub implementations for daemon reporting functions
// These are no-ops when daemon functionality is not available

// ReportTaskStart is a no-op stub when daemon is not available
func ReportTaskStart(client *daemon.Client, taskId int, taskName, hostName string, executionLevel int) error {
	return nil
}

// ReportTaskCompletion is a no-op stub when daemon is not available
func ReportTaskCompletion(client *daemon.Client, task Task, result TaskResult, hostName string, executionLevel int) error {
	return nil
}

// ReportTaskSkipped is a no-op stub when daemon is not available
func ReportTaskSkipped(client *daemon.Client, taskId int, taskName, hostName string, executionLevel int) error {
	return nil
}

// ReportPlayStart is a no-op stub when daemon is not available
func ReportPlayStart(client *daemon.Client, playbook, inventory, executor string) error {
	return nil
}

// ReportPlayCompletion is a no-op stub when daemon is not available
func ReportPlayCompletion(client *daemon.Client) error {
	return nil
}

// ReportPlayError is a no-op stub when daemon is not available
func ReportPlayError(client *daemon.Client, err error) error {
	return nil
}

// WaitForPendingReportsWithTimeout is a no-op stub when daemon is not available
func WaitForPendingReportsWithTimeout(timeout time.Duration) error {
	// No reports to wait for when daemon is not available
	return nil
}
