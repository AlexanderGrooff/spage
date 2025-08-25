package pkg

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/AlexanderGrooff/spage-protobuf/spage/core"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/daemon"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Global WaitGroup to track all reporting goroutines
var reportingWaitGroup sync.WaitGroup

// Utility functions for daemon reporting - use the Client directly

// ReportTaskStart reports the start of a task execution
func ReportTaskStart(client *daemon.Client, taskId int, taskName, hostName string, executionLevel int) error {
	if client == nil {
		return nil
	}

	taskResult := &core.TaskResult{
		TaskId:    int32(taskId),
		TaskName:  taskName,
		Status:    core.TaskStatus_TASK_STATUS_RUNNING,
		StartedAt: timestamppb.Now(),
	}

	reportingWaitGroup.Add(1)
	go func() {
		defer reportingWaitGroup.Done()
		common.LogDebug("Reporting task start", map[string]interface{}{"task_id": taskId, "task_name": taskName, "host_name": hostName})
		if err := client.UpdateTaskResult(taskResult); err != nil {
			common.LogWarn("failed to report task start", map[string]interface{}{"error": err.Error()})
		}
	}()
	return nil
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
		TaskId:      int32(task.Id),
		TaskName:    task.Name,
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
	common.LogInfo("Starting report task completion", map[string]interface{}{
		"task_id":   task.Id,
		"task_name": task.Name,
		"status":    taskStatus,
		"error_msg": errorMsg,
		"output":    result.Output,
		"duration":  result.Duration,
	})
	reportingWaitGroup.Add(1)
	go func() {
		defer reportingWaitGroup.Done()
		common.LogDebug("Reporting task completion", map[string]interface{}{"task_id": task.Id, "task_name": task.Name, "host_name": hostName})
		if err := client.UpdateTaskResult(taskResult); err != nil {
			common.LogWarn("failed to report task completion", map[string]interface{}{"error": err.Error()})
		}
	}()
	return nil
}

func ReportTaskSkipped(client *daemon.Client, taskId int, taskName, hostName string, executionLevel int) error {
	if client == nil {
		return nil
	}

	taskResult := &core.TaskResult{
		TaskId:      int32(taskId),
		TaskName:    taskName,
		Status:      core.TaskStatus_TASK_STATUS_SKIPPED,
		CompletedAt: timestamppb.Now(),
	}

	reportingWaitGroup.Add(1)
	go func() {
		defer reportingWaitGroup.Done()
		common.LogDebug("Reporting task skipped", map[string]interface{}{"task_id": taskId, "task_name": taskName, "host_name": hostName})
		if err := client.UpdateTaskResult(taskResult); err != nil {
			common.LogWarn("failed to report task skipped", map[string]interface{}{"error": err.Error()})
		}
	}()
	return nil
}

func ReportPlayStart(client *daemon.Client, playbook, inventory, executor string) error {
	if client == nil {
		return nil
	}

	reportingWaitGroup.Add(1)
	go func() {
		defer reportingWaitGroup.Done()
		common.LogDebug("Reporting play start", map[string]interface{}{"playbook": playbook, "inventory": inventory, "executor": executor})
		if err := client.RegisterPlayStart(playbook, inventory, map[string]string{}, executor); err != nil {
			common.LogWarn("failed to report play start", map[string]interface{}{"error": err.Error()})
		}
	}()
	return nil
}

func ReportPlayCompletion(client *daemon.Client) error {
	if client == nil {
		return nil
	}
	reportingWaitGroup.Add(1)
	go func() {
		defer reportingWaitGroup.Done()
		common.LogDebug("Reporting play completion", map[string]interface{}{})
		if err := client.RegisterPlayCompletion(); err != nil {
			common.LogWarn("failed to report play completion", map[string]interface{}{"error": err.Error()})
		}
	}()
	return nil
}

func ReportPlayError(client *daemon.Client, err error) error {
	if client == nil {
		return nil
	}
	reportingWaitGroup.Add(1)
	go func() {
		defer reportingWaitGroup.Done()
		common.LogDebug("Reporting play error", map[string]interface{}{"error": err.Error()})
		if err := client.RegisterPlayError(err); err != nil {
			common.LogWarn("failed to report play error", map[string]interface{}{"error": err.Error()})
		}
	}()
	return nil
}

// WaitForPendingReportsWithTimeout waits for all pending daemon reports to complete
// with a timeout to prevent indefinite hanging
func WaitForPendingReportsWithTimeout(timeout time.Duration) error {
	common.LogDebug("waiting for pending reports", map[string]interface{}{"timeout": timeout})
	done := make(chan struct{})
	go func() {
		reportingWaitGroup.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for daemon reports to complete after %v", timeout)
	}
}
