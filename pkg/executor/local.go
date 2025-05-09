package executor

import (
	"context"
	"fmt"
	"github.com/AlexanderGrooff/spage/pkg"
	"strings"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/common"

	"github.com/AlexanderGrooff/spage/pkg/config"
)

// LocalTaskRunner implements the TaskRunner interface for local execution.
// It directly calls the task's ExecuteModule method.
type LocalTaskRunner struct{}

// RunTask executes a task locally.
// It directly calls task.ExecuteModule and returns its result.
// The TaskResult from ExecuteModule is expected to be populated by handleResult (called within ExecuteModule).
func (r *LocalTaskRunner) RunTask(ctx context.Context, task pkg.Task, closure *pkg.Closure, cfg *config.Config) pkg.TaskResult {
	// Check for context cancellation before execution if the task execution itself is long
	// and doesn't frequently check context. However, task.ExecuteModule should ideally handle this.
	select {
	case <-ctx.Done():
		common.LogWarn("Context cancelled before local task execution", map[string]interface{}{
			"task": task.Name, "host": closure.HostContext.Host.Name, "error": ctx.Err(),
		})
		return pkg.TaskResult{
			Task:     task,
			Closure:  closure,
			Error:    fmt.Errorf("task %s on host %s cancelled before local execution: %w", task.Name, closure.HostContext.Host.Name, ctx.Err()),
			Status:   pkg.TaskStatusFailed, // Or a dedicated "cancelled" status
			Failed:   true,
			Duration: 0, // Task didn't run
		}
	default:
	}

	result := task.ExecuteModule(closure)

	// Ensure Task and Closure are set in the result, as ExecuteModule might not always do this
	// (though it should, via the TaskResult it initializes).
	result.Task = task
	result.Closure = closure

	return result
}

// ExecuteWithTimeout wraps ExecuteWithContext with a timeout.
func ExecuteWithTimeout(cfg *config.Config, graph pkg.Graph, inventoryFile string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// Pass the already configured cfg directly
	return ExecuteWithContext(ctx, cfg, graph, inventoryFile)
}

// ExecuteWithContext now uses the BaseExecutor for its core logic.
func ExecuteWithContext(ctx context.Context, cfg *config.Config, graph pkg.Graph, inventoryFile string) error {
	localRunner := &LocalTaskRunner{}
	executor := pkg.NewBaseExecutor(localRunner)

	// The error returned by executor.Execute will be the overall status of the play.
	err := executor.Execute(ctx, cfg, graph, inventoryFile)
	if err != nil {
		// Log the final error from BaseExecutor if it's not just a run failure message
		if !strings.Contains(err.Error(), "run failed") && !strings.Contains(err.Error(), "execution cancelled") {
			common.LogError("Play execution failed with critical error", map[string]interface{}{"error": err.Error()})
		}
		return err // Propagate the error (e.g., "run failed and tasks reverted", or a setup error)
	}
	return nil
}

// Execute executes the graph using the default background context and config.
func Execute(cfg *config.Config, graph pkg.Graph, inventoryFile string) error {
	// Call ExecuteWithContext, which now uses the BaseExecutor.
	err := ExecuteWithContext(context.Background(), cfg, graph, inventoryFile)
	// Debug log for the completion of the Execute call itself.
	// The BaseExecutor handles its own detailed logging.
	common.DebugOutput("Local execution (Execute function) completed.", map[string]interface{}{"error": err})
	return err
}
