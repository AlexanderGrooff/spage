package pkg

import (
	"fmt"
	"runtime"

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

	taskResult := &core.TaskResult{
		TaskId:    taskName, // Use the actual task name
		Status:    core.TaskStatus_TASK_STATUS_RUNNING,
		StartedAt: timestamppb.Now(),
	}

	return client.UpdateTaskResult(taskResult)
}

// ReportTaskCompletion reports the completion of a task with metrics
func ReportTaskCompletion(client *daemon.Client, task Task, result TaskResult, hostName string, executionLevel int) error {
	if client == nil {
		return nil
	}

	// Determine task status based on result
	var taskStatus core.TaskStatus
	var errorMsg string

	if result.Error != nil {
		taskStatus = core.TaskStatus_TASK_STATUS_FAILED
		errorMsg = result.Error.Error()
	} else {
		taskStatus = core.TaskStatus_TASK_STATUS_COMPLETED
	}

	// Create TaskResult from the actual result
	taskResult := &core.TaskResult{
		TaskId:      task.Name, // Use the actual task name
		Status:      taskStatus,
		Error:       errorMsg,
		Output:      fmt.Sprintf("%v", result.Output),
		CompletedAt: timestamppb.Now(),
	}

	// Add metrics if we have them
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if result.Duration > 0 {
		taskResult.Metrics = &core.TaskMetrics{
			Duration:      float64(result.Duration.Seconds()),
			MemoryAlloc:   float64(m.Alloc) / 1024 / 1024,
			MemoryHeap:    float64(m.HeapAlloc) / 1024 / 1024,
			MemorySys:     float64(m.Sys) / 1024 / 1024,
			MemoryHeapSys: float64(m.HeapSys) / 1024 / 1024,
			GcCycles:      float64(m.NumGC),
			Goroutines:    float64(runtime.NumGoroutine()),
			CpuCount:      float64(runtime.NumCPU()),
		}
	}

	// Use the new UpdateTaskResult method with the actual TaskResult
	return client.UpdateTaskResult(taskResult)
}

func ReportTaskSkipped(client *daemon.Client, taskName, hostName string, executionLevel int) error {
	if client == nil {
		return nil
	}

	taskResult := &core.TaskResult{
		TaskId:      taskName, // Use the actual task name
		Status:      core.TaskStatus_TASK_STATUS_SKIPPED,
		CompletedAt: timestamppb.Now(),
	}

	return client.UpdateTaskResult(taskResult)
}

// ReportError reports an error to the daemon
func ReportError(client *daemon.Client, executionLevel int, err error) error {
	if client == nil {
		return nil
	}

	taskResult := &core.TaskResult{
		TaskId:      "error", // Use a generic error task name
		Status:      core.TaskStatus_TASK_STATUS_FAILED,
		Error:       err.Error(),
		CompletedAt: timestamppb.Now(),
	}

	return client.UpdateTaskResult(taskResult)
}

// ReportExecutionStart reports the start of execution
func ReportPlayStart(client *daemon.Client, playbook, inventory, executor string) error {
	return client.RegisterPlayStart(playbook, inventory, map[string]string{}, executor)
}

func ReportPlayCompletion(client *daemon.Client) error {
	return client.RegisterPlayCompletion()
}

func ReportPlayError(client *daemon.Client, err error) error {
	return client.RegisterPlayError(err)
}
