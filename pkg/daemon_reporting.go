package pkg

import (
	"fmt"
	"runtime"
	"time"

	"github.com/AlexanderGrooff/spage-protobuf/spage/core"
	"github.com/AlexanderGrooff/spage/pkg/daemon"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Utility functions for daemon reporting - use the Client directly

// ReportTaskStart reports the start of a task execution
func ReportTaskStart(client *daemon.Client, taskName, hostName string, executionLevel int) error {
	if client == nil {
		return nil
	}

	return client.UpdateProgress(0.0, fmt.Sprintf("Executing task: %s on %s", taskName, hostName), map[string]string{
		"task":   taskName,
		"host":   hostName,
		"level":  fmt.Sprintf("%d", executionLevel),
		"status": "running",
	})
}

// ReportTaskCompletion reports the completion of a task with metrics
func ReportTaskCompletion(client *daemon.Client, task Task, result TaskResult, hostName string, executionLevel int) error {
	if client == nil {
		return nil
	}

	status := "completed"
	errorMsg := ""
	if result.Error != nil {
		status = "failed"
		errorMsg = result.Error.Error()
	}

	// Collect resource metrics
	resourceMetrics := collectResourceMetrics()

	// Create combined metrics
	metrics := map[string]string{
		"task":        task.Name,
		"host":        hostName,
		"level":       fmt.Sprintf("%d", executionLevel),
		"status":      status,
		"duration_ms": fmt.Sprintf("%.2f", float64(result.Duration.Microseconds())/1000.0),
		"duration_ns": fmt.Sprintf("%d", result.Duration.Nanoseconds()),
		"error":       errorMsg,
	}

	// Merge resource metrics
	for k, v := range resourceMetrics {
		metrics[k] = v
	}

	progress := 100.0
	if result.Error != nil {
		progress = -1.0 // Negative progress indicates error
	}

	return client.UpdateProgress(progress, fmt.Sprintf("Task %s %s on %s", task.Name, status, hostName), metrics)
}

func ReportTaskSkipped(client *daemon.Client, taskName, hostName string, executionLevel int) error {
	return client.UpdateProgress(0.0, fmt.Sprintf("Task %s skipped on %s", taskName, hostName), map[string]string{
		"task":   taskName,
		"host":   hostName,
		"level":  fmt.Sprintf("%d", executionLevel),
		"status": "skipped",
	})
}

// ReportTaskProgress reports progress during task processing
func ReportTaskProgress(client *daemon.Client, resultsReceived, numExpectedResultsOnLevel, executionLevel int, result TaskResult) error {
	if client == nil {
		return nil
	}

	// Calculate progress based on completed tasks across all levels
	status := "completed"
	if result.Error != nil {
		status = "failed"
	}

	// Calculate progress percentage
	progress := float64(resultsReceived) * 100.0 / float64(numExpectedResultsOnLevel)
	if progress > 100.0 {
		progress = 100.0
	}

	if result.Error != nil {
		progress = -1.0 // Negative progress indicates error
	}

	return client.UpdateProgress(progress, fmt.Sprintf("Task %s %s", result.Task.Name, status), map[string]string{
		"task":            result.Task.Name,
		"host":            result.Closure.HostContext.Host.Name,
		"level":           fmt.Sprintf("%d", executionLevel),
		"status":          status,
		"tasks_completed": fmt.Sprintf("%d", resultsReceived),
		"tasks_total":     fmt.Sprintf("%d", numExpectedResultsOnLevel),
	})
}

// ReportError reports an error to the daemon
func ReportError(client *daemon.Client, executionLevel int, err error) error {
	if client == nil {
		return nil
	}

	return client.UpdateProgress(-1.0, fmt.Sprintf("Error in level %d: %s", executionLevel, err.Error()), map[string]string{
		"level":  fmt.Sprintf("%d", executionLevel),
		"error":  err.Error(),
		"status": "failed",
	})
}

// ReportExecutionStart reports the start of execution
func ReportPlayStart(client *daemon.Client, playbook, inventory, executor string) error {
	return client.RegisterPlayStart(playbook, inventory, map[string]string{}, executor)
}

// collectResourceMetrics collects system resource metrics
func collectResourceMetrics() map[string]string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]string{
		"memory_alloc_mb":    fmt.Sprintf("%.2f", float64(m.Alloc)/1024/1024),
		"memory_total_mb":    fmt.Sprintf("%.2f", float64(m.TotalAlloc)/1024/1024),
		"memory_heap_mb":     fmt.Sprintf("%.2f", float64(m.HeapAlloc)/1024/1024),
		"memory_sys_mb":      fmt.Sprintf("%.2f", float64(m.Sys)/1024/1024),
		"memory_heap_sys_mb": fmt.Sprintf("%.2f", float64(m.HeapSys)/1024/1024),
		"gc_cycles":          fmt.Sprintf("%d", m.NumGC),
		"goroutines":         fmt.Sprintf("%d", runtime.NumGoroutine()),
		"cpu_count":          fmt.Sprintf("%d", runtime.NumCPU()),
	}
}

// CreateProgressUpdate creates a proper ProgressUpdate protobuf message
func CreateProgressUpdate(taskID string, progress float64, message string, metadata map[string]string) *core.ProgressUpdate {
	return &core.ProgressUpdate{
		TaskId:    taskID,
		Progress:  progress,
		Message:   message,
		Metadata:  metadata,
		Timestamp: timestamppb.Now(),
	}
}

// CreateTaskResult creates a proper TaskResult protobuf message
func CreateTaskResult(taskID string, status core.TaskStatus, output string, err error, duration time.Duration, result map[string]string) *core.TaskResult {
	taskResult := &core.TaskResult{
		TaskId:    taskID,
		Status:    status,
		Output:    output,
		Duration:  duration.Seconds(),
		Result:    result,
		StartedAt: timestamppb.Now(), // This should be set when task starts
	}

	if err != nil {
		taskResult.Error = err.Error()
		taskResult.Status = core.TaskStatus_TASK_STATUS_FAILED
	}

	return taskResult
}

// CreateTask creates a proper Task protobuf message
func CreateTask(taskID, taskType string, priority core.TaskPriority, payload map[string]string, status core.TaskStatus, progress float64, result map[string]string, err string) *core.Task {
	task := &core.Task{
		Id:        taskID,
		Type:      taskType,
		Priority:  priority,
		Payload:   payload,
		Status:    status,
		Progress:  progress,
		Result:    result,
		Error:     err,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
	}

	return task
}
