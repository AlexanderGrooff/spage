package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
)

// LocalTaskRunner implements the TaskRunner interface for local execution.
// It directly calls the task's ExecuteModule method.
type LocalTaskRunner struct{}

// LocalGraphExecutor provides common logic for executing a Spage graph.
// It relies on a TaskRunner to perform the actual execution of individual tasks.
type LocalGraphExecutor struct {
	Runner pkg.TaskRunner
}

// NewLocalGraphExecutor creates a new LocalGraphExecutor with the given TaskRunner.
func NewLocalGraphExecutor(runner pkg.TaskRunner) *LocalGraphExecutor {
	return &LocalGraphExecutor{Runner: runner}
}

// RunTask executes a task locally.
// It directly calls task.ExecuteModule and returns its result.
// The TaskResult from ExecuteModule is expected to be populated by handleResult (called within ExecuteModule).
func (r *LocalTaskRunner) ExecuteTask(ctx context.Context, task pkg.GraphNode, closure *pkg.Closure, cfg *config.Config) chan pkg.TaskResult {
	// Check for context cancellation before execution if the task execution itself is long
	// and doesn't frequently check context. However, task.ExecuteModule should ideally handle this.
	select {
	case <-ctx.Done():
		common.LogWarn("Context cancelled before local task execution", map[string]interface{}{
			"task": task.Params().Name, "host": closure.HostContext.Host.Name, "error": ctx.Err(),
		})
		ch := make(chan pkg.TaskResult)
		ch <- pkg.TaskResult{
			Task:     task,
			Closure:  closure,
			Error:    fmt.Errorf("task %s on host %s cancelled before local execution: %w", task.Params().Name, closure.HostContext.Host.Name, ctx.Err()),
			Status:   pkg.TaskStatusFailed, // Or a dedicated "cancelled" status
			Failed:   true,
			Duration: 0, // Task didn't run
		}
		return ch
	default:
	}

	return task.ExecuteModule(closure)
}

func (r *LocalTaskRunner) RevertTask(ctx context.Context, task pkg.GraphNode, closure *pkg.Closure, cfg *config.Config) chan pkg.TaskResult {
	// Check for context cancellation before execution if the task execution itself is long
	// and doesn't frequently check context. However, task.ExecuteModule should ideally handle this.
	select {
	case <-ctx.Done():
		common.LogWarn("Context cancelled before local task execution", map[string]interface{}{
			"task": task.Params().Name, "host": closure.HostContext.Host.Name, "error": ctx.Err(),
		})
		ch := make(chan pkg.TaskResult)
		ch <- pkg.TaskResult{
			Task:     task,
			Closure:  closure,
			Error:    fmt.Errorf("task %s on host %s cancelled before local execution: %w", task.Params().Name, closure.HostContext.Host.Name, ctx.Err()),
			Status:   pkg.TaskStatusFailed, // Or a dedicated "cancelled" status
			Failed:   true,
			Duration: 0, // Task didn't run
		}
		return ch
	default:
	}

	return task.RevertModule(closure)
}

func (e *LocalGraphExecutor) Execute(hostContexts map[string]*pkg.HostContext, orderedGraph [][]pkg.GraphNode, cfg *config.Config) error {
	common.LogDebug("Executing local graph", map[string]interface{}{})
	ctx := context.Background()
	recapStats := InitializeRecapStats(hostContexts)
	var executionHistory []map[string]chan pkg.GraphNode // For revert functionality
	for executionLevel, tasksInLevel := range orderedGraph {
		select {
		case <-ctx.Done():
			return fmt.Errorf("execution cancelled before level %d: %w", executionLevel, ctx.Err())
		default:
		}

		levelHistoryForRevert, numExpectedResultsOnLevel, err := PrepareLevelHistoryAndGetCount(tasksInLevel, hostContexts, executionLevel, cfg)
		if err != nil {
			return fmt.Errorf("failed to prepare level history and get count: %w", err)
		}
		executionHistory = append(executionHistory, levelHistoryForRevert)

		resultsCh := make(chan pkg.TaskResult, numExpectedResultsOnLevel)
		errCh := make(chan error, 1) // For fatal errors from the task loading goroutine

		var wg sync.WaitGroup
		go e.loadLevelTasks(ctx, &wg, tasksInLevel, hostContexts, resultsCh, errCh, cfg, executionLevel)
		wg.Wait()

		levelErrored, errProcessingResults := e.processLevelResults(
			ctx, resultsCh, errCh,
			recapStats, executionHistory, executionLevel,
			cfg, numExpectedResultsOnLevel,
		)
		if errProcessingResults != nil {
			return errProcessingResults
		}

		if levelErrored {
			// Execute handlers before reverting, as handlers should run for any tasks that successfully notified them
			if err := e.executeHandlers(ctx, hostContexts, recapStats, cfg); err != nil {
				common.LogWarn("Failed to execute handlers before revert", map[string]interface{}{"error": err.Error()})
			}

			if !cfg.Revert {
				return fmt.Errorf("run failed on level %d, revert is disabled by config", executionLevel)
			}
			if cfg.Logging.Format == "plain" {
				fmt.Printf("\nREVERTING TASKS **********************************************\n")
			} else {
				common.LogInfo("Run failed, starting task reversion", map[string]interface{}{"level": executionLevel})
			}
			if errRevert := e.Revert(ctx, executionHistory, hostContexts, cfg); errRevert != nil {
				return fmt.Errorf("run failed on level %d and also failed during revert: %w (original error trigger) | %v (revert error)", executionLevel, errors.New("task failure"), errRevert)
			}
			return fmt.Errorf("run failed on level %d and tasks reverted", executionLevel)
		}
	}

	// Execute handlers after all regular tasks complete
	if err := e.executeHandlers(ctx, hostContexts, recapStats, cfg); err != nil {
		return fmt.Errorf("failed to execute handlers: %w", err)
	}

	e.printPlayRecap(cfg, recapStats)
	return nil
}

func (e *LocalGraphExecutor) Revert(ctx context.Context, executedTasks []map[string]chan pkg.GraphNode, hostContexts map[string]*pkg.HostContext, cfg *config.Config) error {
	recapStats := make(map[string]map[string]int)
	for hostname := range hostContexts {
		recapStats[hostname] = map[string]int{"ok": 0, "changed": 0, "failed": 0}
	}

	for executionLevel := len(executedTasks) - 1; executionLevel >= 0; executionLevel-- {
		common.DebugOutput("Reverting level %d", executionLevel)
		levelTasksByHost := executedTasks[executionLevel]

		for hostname, taskHistoryCh := range levelTasksByHost {
			common.DebugOutput("Reverting host '%s' on level %d", hostname, executionLevel)
			hostCtx, contextExists := hostContexts[hostname]
			if !contextExists {
				common.LogError("Context not found for host during revert, skipping host", map[string]interface{}{"host": hostname, "level": executionLevel})
				// How to count this in recap? For now, it's a skip for this host's tasks on this level.
				continue
			}

			// Drain the channel for this host and level
			for taskNode := range taskHistoryCh { // taskHistoryCh should be closed by processLevelResults
				// Accept both pointer and value Task types, and skip MetaTask in both forms
				var task *pkg.Task
				switch tn := taskNode.(type) {
				case *pkg.Task:
					task = tn
				case pkg.Task:
					task = &tn
				case *pkg.MetaTask:
					common.LogDebug("Skipping meta task in Revert (no revert needed)", map[string]interface{}{"node": taskNode.String()})
					continue
				// Note: non-pointer MetaTask does not implement GraphNode (pointer methods), so it won't appear here
				default:
					// TODO: handle other non-task nodes
					common.LogWarn("Skipping non-task node in Revert", map[string]interface{}{"node": taskNode.String()})
					continue
				}
				if cfg.Logging.Format == "plain" {
					fmt.Printf("\nREVERT TASK [%s] (%s) *****************************************\n", task.GetName(), hostname)
				} else {
					common.LogInfo("Attempting to revert task", map[string]interface{}{"task": task.GetName(), "host": hostname, "level": executionLevel})
				}
				logData := map[string]interface{}{
					"host": hostname, "task": task.GetName(), "action": "revert",
				}

				// Check if the task's module parameters implement HasRevert
				if P, ok := task.Params().Params.Actual.(interface{ HasRevert() bool }); !ok || !P.HasRevert() {
					logData["status"] = "ok"
					logData["changed"] = false
					logData["message"] = "No revert defined for module or params"
					recapStats[hostname]["ok"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("ok: [%s] => (no revert defined)\n", hostname)
					} else {
						common.LogInfo("Skipping revert (no revert defined for module/params)", logData)
					}
					continue
				}

				closure := task.ConstructClosure(hostCtx, cfg)
				revertCh := task.RevertModule(closure) // Calls module's Revert via Task.RevertModule
				revertResult := <-revertCh

				if revertResult.Error != nil {
					logData["status"] = "failed"
					logData["error"] = revertResult.Error.Error()
					if revertResult.Output != nil {
						logData["output"] = revertResult.Output.String()
					}
					recapStats[hostname]["failed"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("failed: [%s] => (%v)\n", hostname, revertResult.Error)
						PPrintOutput(revertResult.Output, revertResult.Error)
					} else {
						common.LogError("Revert task failed", logData)
					}
				} else if revertResult.Output != nil && revertResult.Output.Changed() {
					logData["status"] = "changed"
					logData["changed"] = true
					if revertResult.Output != nil {
						logData["output"] = revertResult.Output.String()
					}
					recapStats[hostname]["changed"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("changed: [%s] => \n%v\n", hostname, revertResult.Output)
					} else {
						common.LogInfo("Revert task changed", logData)
					}
				} else { // OK
					logData["status"] = "ok"
					logData["changed"] = false
					if revertResult.Output != nil {
						logData["output"] = revertResult.Output.String()
					}
					recapStats[hostname]["ok"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("ok: [%s]\n", hostname)
						PPrintOutput(revertResult.Output, nil)
					} else {
						common.LogInfo("Revert task ok", logData)
					}
				}
			}
			common.DebugOutput("Finished reverting host '%s' on level %d", hostname, executionLevel)
		}
		common.DebugOutput("Finished reverting level %d", executionLevel)
	}

	if cfg.Logging.Format == "plain" {
		fmt.Printf("\nREVERT RECAP ****************************************************\n")
		for hostname, stats := range recapStats {
			fmt.Printf("%s : ok=%d    changed=%d    failed=%d\n", hostname, stats["ok"], stats["changed"], stats["failed"])
		}
	} else {
		common.LogInfo("Revert recap", map[string]interface{}{"stats": recapStats})
	}

	for _, stats := range recapStats {
		if stats["failed"] > 0 {
			return fmt.Errorf("one or more tasks failed to revert")
		}
	}
	common.DebugOutput("Revert process completed successfully.")
	return nil
}

func (e *LocalGraphExecutor) loadLevelTasks(
	ctx context.Context,
	wg *sync.WaitGroup,
	tasksInLevel []pkg.GraphNode,
	hostContexts map[string]*pkg.HostContext,
	resultsCh chan pkg.TaskResult,
	errCh chan error,
	cfg *config.Config,
	executionLevel int,
) {
	// Use shared generic loader
	env := NewLocalDispatchEnv(ctx, wg, resultsCh, errCh, cfg.ExecutionMode == "parallel")
	runner := NewLocalRunnerAdapter(ctx, e.Runner, cfg)
	SharedLoadLevelTasks(env, runner, tasksInLevel, hostContexts, cfg, nil)
	// Close results channel after dispatch/wait completes
	close(resultsCh)
}

// executeHandlers runs all notified handlers across all hosts
func (e *LocalGraphExecutor) executeHandlers(
	ctx context.Context,
	hostContexts map[string]*pkg.HostContext,
	recapStats map[string]map[string]int,
	cfg *config.Config,
) error {
	// Check if there are any handlers to execute
	hasHandlers := false
	for _, hostCtx := range hostContexts {
		if hostCtx.HandlerTracker != nil {
			notifiedHandlers := hostCtx.HandlerTracker.GetNotifiedHandlers()
			if len(notifiedHandlers) > 0 {
				hasHandlers = true
				break
			}
		}
	}

	if !hasHandlers {
		common.LogDebug("No handlers to execute", map[string]interface{}{})
		return nil
	}

	if cfg.Logging.Format == "plain" {
		fmt.Printf("\nRUNNING HANDLERS *********************************************\n")
	} else {
		common.LogInfo("Running handlers", map[string]interface{}{})
	}

	// Execute handlers for each host
	for hostname, hostCtx := range hostContexts {
		if hostCtx.HandlerTracker == nil {
			continue
		}

		notifiedHandlers := hostCtx.HandlerTracker.GetNotifiedHandlers()
		if len(notifiedHandlers) == 0 {
			continue
		}

		common.LogDebug("Executing handlers for host", map[string]interface{}{
			"host":     hostname,
			"handlers": len(notifiedHandlers),
		})

		for _, handler := range notifiedHandlers {
			task, ok := handler.(*pkg.Task)
			if !ok {
				// TODO: handle non-task nodes
				common.LogWarn("Skipping non-task node in executeHandlers", map[string]interface{}{"node": handler.String()})
				continue
			}

			// Skip if already executed
			if hostCtx.HandlerTracker.IsExecuted(handler.Params().Name) {
				continue
			}

			// Execute the handler
			closure := handler.ConstructClosure(hostCtx, cfg)
			// TODO: this assumes a single result
			resultCh := e.Runner.ExecuteTask(ctx, task, closure, cfg)
			result := <-resultCh

			// Mark the handler as executed
			hostCtx.HandlerTracker.MarkExecuted(handler.Params().Name)

			// Process the handler result using shared logic
			processor := &ResultProcessor{
				ExecutionLevel: -1, // Handlers don't have execution levels
				Logger:         NewLocalLogger(),
				Config:         cfg,
			}
			processor.ProcessHandlerResult(result, recapStats)
		}
	}

	return nil
}

func (e *LocalGraphExecutor) processLevelResults(
	ctx context.Context,
	resultsCh chan pkg.TaskResult,
	errCh chan error,
	recapStats map[string]map[string]int,
	executionHistory []map[string]chan pkg.GraphNode,
	executionLevel int,
	cfg *config.Config,
	numExpectedResultsOnLevel int,
) (bool, error) {
	defer func() {
		histEntry := executionHistory[executionLevel]
		for _, hostChan := range histEntry {
			close(hostChan)
		}
	}()

	// Create adapters for the shared function
	resultsChAdapter := NewLocalResultChannel(resultsCh)
	errChAdapter := NewLocalErrorChannel(errCh)
	logger := NewLocalLogger()

	// Create a context-aware result channel that handles cancellation
	ctxResultsCh := NewContextAwareResultChannel(ctx, resultsChAdapter)
	ctxErrCh := NewContextAwareErrorChannel(ctx, errChAdapter)

	common.LogDebug("Processing level results", map[string]interface{}{"execution_level": executionLevel, "num_expected_results": numExpectedResultsOnLevel})
	levelHardErrored, _, err := SharedProcessLevelResults(
		ctxResultsCh,
		ctxErrCh,
		logger,
		executionLevel,
		cfg,
		numExpectedResultsOnLevel,
		recapStats,
		executionHistory[executionLevel], // Pass the execution history for this level
		nil,                              // No additional processing needed for local executor
	)

	return levelHardErrored, err
}

func (e *LocalGraphExecutor) printPlayRecap(cfg *config.Config, recapStats map[string]map[string]int) {
	if cfg.Logging.Format == "plain" {
		fmt.Printf("\nPLAY RECAP ****************************************************\n")
		for hostname, stats := range recapStats {
			okCount := stats["ok"]
			changedCount := stats["changed"]
			failedCount := stats["failed"]
			skippedCount := stats["skipped"]
			ignoredCount := stats["ignored"]
			fmt.Printf("%s : ok=%d    changed=%d    failed=%d    skipped=%d    ignored=%d\n",
				hostname, okCount, changedCount, failedCount, skippedCount, ignoredCount)
		}
	} else {
		common.LogInfo("Play recap", map[string]interface{}{"stats": recapStats})
	}
}
