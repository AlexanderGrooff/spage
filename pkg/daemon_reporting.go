package pkg

import (
	"fmt"
	"runtime"
)

// DaemonReporter interface for progress and metrics reporting
type DaemonReporter interface {
	UpdateProgress(message string, metadata map[string]string) error
}

// DaemonReporting provides centralized daemon reporting functionality
type DaemonReporting struct {
	reporter DaemonReporter
}

// NewDaemonReporting creates a new DaemonReporting instance
func NewDaemonReporting(reporter DaemonReporter) *DaemonReporting {
	return &DaemonReporting{
		reporter: reporter,
	}
}

// ReportTaskStart reports the start of a task execution
func (d *DaemonReporting) ReportTaskStart(taskName, hostName string, executionLevel int) error {
	if d.reporter == nil {
		return nil
	}

	return d.reporter.UpdateProgress(fmt.Sprintf("Executing task: %s on %s", taskName, hostName), map[string]string{
		"task":  taskName,
		"host":  hostName,
		"level": fmt.Sprintf("%d", executionLevel),
	})
}

// ReportTaskCompletion reports the completion of a task with metrics
func (d *DaemonReporting) ReportTaskCompletion(task Task, result TaskResult, hostName string, executionLevel int, executionMode string, taskType string) error {
	if d.reporter == nil {
		return nil
	}

	status := "completed"
	errorMsg := ""
	if result.Error != nil {
		status = "failed"
		errorMsg = result.Error.Error()
	}

	// Collect resource metrics
	resourceMetrics := d.collectResourceMetrics()

	// Create combined metrics
	metrics := map[string]string{
		"task":           task.Name,
		"host":           hostName,
		"level":          fmt.Sprintf("%d", executionLevel),
		"status":         status,
		"duration_ms":    fmt.Sprintf("%.2f", float64(result.Duration.Microseconds())/1000.0),
		"duration_ns":    fmt.Sprintf("%d", result.Duration.Nanoseconds()),
		"error":          errorMsg,
		"task_type":      taskType,
		"execution_mode": executionMode,
	}

	// Merge resource metrics
	for k, v := range resourceMetrics {
		metrics[k] = v
	}

	return d.reporter.UpdateProgress(fmt.Sprintf("Task %s %s on %s", task.Name, status, hostName), metrics)
}

// ReportRunOnceItemCompletion reports completion of a run-once loop item
func (d *DaemonReporting) ReportRunOnceItemCompletion(task Task, result TaskResult, hostName string, executionLevel int, itemCount int) error {
	if d.reporter == nil {
		return nil
	}

	status := "completed"
	errorMsg := ""
	if result.Error != nil {
		status = "failed"
		errorMsg = result.Error.Error()
	}

	// Collect resource metrics
	resourceMetrics := d.collectResourceMetrics()

	// Create combined metrics
	metrics := map[string]string{
		"task":           task.Name,
		"host":           hostName,
		"level":          fmt.Sprintf("%d", executionLevel),
		"status":         status,
		"duration_ms":    fmt.Sprintf("%.2f", float64(result.Duration.Microseconds())/1000.0),
		"duration_ns":    fmt.Sprintf("%d", result.Duration.Nanoseconds()),
		"error":          errorMsg,
		"task_type":      "run_once_item",
		"execution_mode": "loop",
		"item_count":     fmt.Sprintf("%d", itemCount),
	}

	// Merge resource metrics
	for k, v := range resourceMetrics {
		metrics[k] = v
	}

	return d.reporter.UpdateProgress(fmt.Sprintf("Run-once item %s %s on %s", task.Name, status, hostName), metrics)
}

// ReportRunOnceSingleCompletion reports completion of a single run-once task
func (d *DaemonReporting) ReportRunOnceSingleCompletion(task Task, result TaskResult, hostName string, executionLevel int) error {
	if d.reporter == nil {
		return nil
	}

	status := "completed"
	errorMsg := ""
	if result.Error != nil {
		status = "failed"
		errorMsg = result.Error.Error()
	}

	// Collect resource metrics
	resourceMetrics := d.collectResourceMetrics()

	// Create combined metrics
	metrics := map[string]string{
		"task":           task.Name,
		"host":           hostName,
		"level":          fmt.Sprintf("%d", executionLevel),
		"status":         status,
		"duration_ms":    fmt.Sprintf("%.2f", float64(result.Duration.Microseconds())/1000.0),
		"duration_ns":    fmt.Sprintf("%d", result.Duration.Nanoseconds()),
		"error":          errorMsg,
		"task_type":      "run_once_single",
		"execution_mode": "single",
	}

	// Merge resource metrics
	for k, v := range resourceMetrics {
		metrics[k] = v
	}

	return d.reporter.UpdateProgress(fmt.Sprintf("Run-once task %s %s on %s", task.Name, status, hostName), metrics)
}

// ReportLevelProcessing reports progress during level processing
func (d *DaemonReporting) ReportLevelProcessing(executionLevel, tasksInLevel int) error {
	if d.reporter == nil {
		return nil
	}

	return d.reporter.UpdateProgress(fmt.Sprintf("Processing level %d", executionLevel), map[string]string{
		"level":          fmt.Sprintf("%d", executionLevel),
		"tasks_in_level": fmt.Sprintf("%d", tasksInLevel),
		"phase":          "processing",
	})
}

// ReportTaskProgress reports progress during task processing
func (d *DaemonReporting) ReportTaskProgress(resultsReceived, numExpectedResultsOnLevel, executionLevel int, result TaskResult) error {
	if d.reporter == nil {
		return nil
	}

	// Calculate progress based on completed tasks across all levels
	status := "completed"
	if result.Error != nil {
		status = "failed"
	}

	return d.reporter.UpdateProgress(fmt.Sprintf("Task %s %s", result.Task.Name, status), map[string]string{
		"task":            result.Task.Name,
		"host":            result.Closure.HostContext.Host.Name,
		"level":           fmt.Sprintf("%d", executionLevel),
		"status":          status,
		"tasks_completed": fmt.Sprintf("%d", resultsReceived),
		"tasks_total":     fmt.Sprintf("%d", numExpectedResultsOnLevel),
	})
}

// ReportError reports an error to the daemon
func (d *DaemonReporting) ReportError(executionLevel int, err error) error {
	if d.reporter == nil {
		return nil
	}

	return d.reporter.UpdateProgress(fmt.Sprintf("Error in level %d: %s", executionLevel, err.Error()), map[string]string{
		"level":  fmt.Sprintf("%d", executionLevel),
		"error":  err.Error(),
		"status": "failed",
	})
}

// ReportExecutionStart reports the start of execution
func (d *DaemonReporting) ReportExecutionStart(graph *Graph) error {
	if d.reporter == nil {
		return nil
	}

	totalTasks := 0
	for _, level := range graph.Tasks {
		totalTasks += len(level)
	}
	return d.reporter.UpdateProgress("Starting execution", map[string]string{
		"total_tasks": fmt.Sprintf("%d", totalTasks),
		"levels":      fmt.Sprintf("%d", len(graph.Tasks)),
	})
}

// ReportExecutionCompletion reports the completion of execution
func (d *DaemonReporting) ReportExecutionCompletion() error {
	if d.reporter == nil {
		return nil
	}

	return d.reporter.UpdateProgress("Play finished successfully", map[string]string{
		"status": "completed",
	})
}

// collectResourceMetrics collects system resource metrics
func (d *DaemonReporting) collectResourceMetrics() map[string]string {
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
