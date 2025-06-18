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
func (r *LocalTaskRunner) ExecuteTask(ctx context.Context, task pkg.Task, closure *pkg.Closure, cfg *config.Config) pkg.TaskResult {
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

func (r *LocalTaskRunner) RevertTask(ctx context.Context, task pkg.Task, closure *pkg.Closure, cfg *config.Config) pkg.TaskResult {
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

	result := task.RevertModule(closure)

	// Ensure Task and Closure are set in the result, as ExecuteModule might not always do this
	// (though it should, via the TaskResult it initializes).
	result.Task = task
	result.Closure = closure

	return result
}

func (e *LocalGraphExecutor) Execute(hostContexts map[string]*pkg.HostContext, orderedGraph [][]pkg.Task, cfg *config.Config) error {
	ctx := context.Background()
	recapStats := InitializeRecapStats(hostContexts)
	var executionHistory []map[string]chan pkg.Task // For revert functionality
	for executionLevel, tasksInLevel := range orderedGraph {
		select {
		case <-ctx.Done():
			return fmt.Errorf("execution cancelled before level %d: %w", executionLevel, ctx.Err())
		default:
		}

		levelHistoryForRevert, numExpectedResultsOnLevel, err := PrepareLevelHistoryAndGetCount(tasksInLevel, hostContexts, executionLevel)
		if err != nil {
			return fmt.Errorf("failed to prepare level history and get count: %w", err)
		}
		executionHistory = append(executionHistory, levelHistoryForRevert)

		resultsCh := make(chan pkg.TaskResult, numExpectedResultsOnLevel)
		errCh := make(chan error, 1) // For fatal errors from the task loading goroutine

		common.DebugOutput("Scheduling %d task instances on level %d", numExpectedResultsOnLevel, executionLevel)

		go e.loadLevelTasks(ctx, tasksInLevel, hostContexts, resultsCh, errCh, cfg)

		levelErrored, errProcessingResults := e.processLevelResults(
			ctx, resultsCh, errCh,
			recapStats, executionHistory, executionLevel,
			cfg, numExpectedResultsOnLevel,
		)
		if errProcessingResults != nil {
			return errProcessingResults
		}

		if levelErrored {
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

	e.printPlayRecap(cfg, recapStats)
	common.LogInfo("Play finished successfully.", map[string]interface{}{})
	return nil
}

func (e *LocalGraphExecutor) Revert(ctx context.Context, executedTasks []map[string]chan pkg.Task, hostContexts map[string]*pkg.HostContext, cfg *config.Config) error {
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
			for task := range taskHistoryCh { // taskHistoryCh should be closed by processLevelResults
				if cfg.Logging.Format == "plain" {
					fmt.Printf("\nREVERT TASK [%s] (%s) *****************************************\n", task.Name, hostname)
				} else {
					common.LogInfo("Attempting to revert task", map[string]interface{}{"task": task.Name, "host": hostname, "level": executionLevel})
				}
				logData := map[string]interface{}{
					"host": hostname, "task": task.Name, "action": "revert",
				}

				// Check if the task's module parameters implement HasRevert
				if P, ok := task.Params.Actual.(interface{ HasRevert() bool }); !ok || !P.HasRevert() {
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

				closure := pkg.ConstructClosure(hostCtx, task)
				revertResult := task.RevertModule(closure) // Calls module's Revert via Task.RevertModule

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
	tasksInLevel []pkg.Task,
	hostContexts map[string]*pkg.HostContext,
	resultsCh chan pkg.TaskResult,
	errCh chan error,
	cfg *config.Config,
) {
	defer close(resultsCh)

	var wg sync.WaitGroup
	isParallelDispatch := cfg.ExecutionMode == "parallel"

	for _, taskDefinition := range tasksInLevel {
		task := taskDefinition

		// Handle run_once tasks separately
		if task.RunOnce {
			// Get the first available host
			firstHostCtx, firstHostName := GetFirstAvailableHost(hostContexts)
			if firstHostCtx == nil {
				errMsg := fmt.Errorf("no hosts available for run_once task '%s'", task.Name)
				common.LogError("No hosts available for run_once task", map[string]interface{}{"error": errMsg})
				select {
				case errCh <- errMsg:
				case <-ctx.Done():
				}
				return
			}

			// Execute only on the first host
			closures, err := GetTaskClosures(task, firstHostCtx)
			if err != nil {
				errMsg := fmt.Errorf("critical error: failed to get task closures for run_once task '%s' on host '%s': %w", task.Name, firstHostName, err)
				common.LogError("Dispatch error for run_once task", map[string]interface{}{"error": errMsg})
				select {
				case errCh <- errMsg:
				case <-ctx.Done():
				}
				return
			}

			for _, individualClosure := range closures {
				closure := individualClosure

				// Resolve delegate_to if specified
				if task.DelegateTo != "" {
					delegatedHostContext, err := GetDelegatedHostContext(task, hostContexts, closure)
					if err != nil {
						errMsg := fmt.Errorf("failed to resolve delegate_to for run_once task '%s': %w", task.Name, err)
						common.LogError("Delegate resolution error for run_once task", map[string]interface{}{"error": errMsg})
						select {
						case errCh <- errMsg:
						case <-ctx.Done():
						}
						return
					}
					if delegatedHostContext != nil {
						// Update the closure to use the delegated host context
						closure.HostContext = delegatedHostContext
					}
				}

				select {
				case <-ctx.Done():
					common.LogWarn("Context cancelled, stopping run_once task dispatch", map[string]interface{}{"task": task.Name, "host": firstHostName})
					select {
					case errCh <- fmt.Errorf("run_once task dispatch cancelled for task '%s' on host '%s': %w", task.Name, firstHostName, ctx.Err()):
					default:
					}
					return
				default:
				}

				common.LogInfo("Executing run_once task", map[string]interface{}{
					"task":           task.Name,
					"execution_host": firstHostName,
					"total_hosts":    len(hostContexts),
				})

				// Execute the task on the first host
				originalResult := e.Runner.ExecuteTask(ctx, task, closure, cfg)

				// Create results for all hosts based on the original execution
				allResults := CreateRunOnceResultsForAllHosts(originalResult, hostContexts, firstHostName)

				// Send results for all hosts
				for _, result := range allResults {
					select {
					case resultsCh <- result:
					case <-ctx.Done():
						return
					}
				}
			}

			// Continue to next task since run_once is handled
			continue
		}

		// Normal task execution for non-run_once tasks
		for hostNameKey, hostCtxInstance := range hostContexts {
			hostCtx := hostCtxInstance
			hostName := hostNameKey

			closures, err := GetTaskClosures(task, hostCtx)
			if err != nil {
				errMsg := fmt.Errorf("critical error: failed to get task closures for task '%s' on host '%s': %w. Aborting level.", task.Name, hostName, err)
				common.LogError("Dispatch error in loadLevelTasks", map[string]interface{}{"error": errMsg})
				select {
				case errCh <- errMsg:
				case <-ctx.Done():
				}
				return
			}

			for _, individualClosure := range closures {
				closure := individualClosure

				// Resolve delegate_to if specified
				if task.DelegateTo != "" {
					delegatedHostContext, err := GetDelegatedHostContext(task, hostContexts, closure)
					if err != nil {
						errMsg := fmt.Errorf("failed to resolve delegate_to for task '%s': %w", task.Name, err)
						common.LogError("Delegate resolution error in loadLevelTasks", map[string]interface{}{"error": errMsg})
						select {
						case errCh <- errMsg:
						case <-ctx.Done():
						}
						return
					}
					if delegatedHostContext != nil {
						// Update the closure to use the delegated host context
						closure.HostContext = delegatedHostContext
					}
				}

				select {
				case <-ctx.Done():
					common.LogWarn("Context cancelled, stopping task dispatch for level.", map[string]interface{}{"task": task.Name, "host": hostName})
					select {
					case errCh <- fmt.Errorf("dispatch loop cancelled for task '%s' on host '%s': %w", task.Name, hostName, ctx.Err()):
					default:
					}
					return
				default:
				}

				if isParallelDispatch {
					wg.Add(1)
					go func() {
						defer wg.Done()
						select {
						case <-ctx.Done():
							return
						default:
							taskResult := e.Runner.ExecuteTask(ctx, task, closure, cfg)
							select {
							case resultsCh <- taskResult:
							case <-ctx.Done():
							}
						}
					}()
				} else {
					taskResult := e.Runner.ExecuteTask(ctx, task, closure, cfg)
					select {
					case resultsCh <- taskResult:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}

	if isParallelDispatch {
		wg.Wait()
	}
}

func (e *LocalGraphExecutor) processLevelResults(
	ctx context.Context,
	resultsCh chan pkg.TaskResult,
	errCh chan error,
	recapStats map[string]map[string]int,
	executionHistory []map[string]chan pkg.Task,
	executionLevel int,
	cfg *config.Config,
	numExpectedResultsOnLevel int,
) (bool, error) {
	var levelHardErrored bool
	resultsReceived := 0

	defer func() {
		histEntry := executionHistory[executionLevel]
		for _, hostChan := range histEntry {
			close(hostChan)
		}
	}()

	for resultsReceived < numExpectedResultsOnLevel {
		select {
		case result, ok := <-resultsCh:
			if !ok {
				if resultsReceived < numExpectedResultsOnLevel && ctx.Err() == nil && !levelHardErrored {
					select {
					case dispatchErr := <-errCh:
						if dispatchErr != nil {
							return true, fmt.Errorf("task dispatch failed on level %d: %w. Expected %d results, got %d.", executionLevel, dispatchErr, numExpectedResultsOnLevel, resultsReceived)
						}
					default:
					}
					return true, fmt.Errorf("results channel closed prematurely on level %d. Expected %d, got %d. No specific dispatch error or context cancellation.", executionLevel, numExpectedResultsOnLevel, resultsReceived)
				}
				goto endProcessingLoop
			}

			resultsReceived++
			hostname := result.Closure.HostContext.Host.Name
			task := result.Task
			currentClosure := result.Closure

			// Ensure recapStats entry exists for this hostname (e.g., for delegate_to localhost)
			if _, exists := recapStats[hostname]; !exists {
				recapStats[hostname] = map[string]int{"ok": 0, "changed": 0, "failed": 0, "skipped": 0, "ignored": 0}
			}

			if cfg.Logging.Format == "plain" {
				fmt.Printf("\nTASK [%s] (%s) ****************************************************\n", task.Name, hostname)
			}

			levelHistMap := executionHistory[executionLevel]
			if hostChan, ok := levelHistMap[hostname]; ok {
				select {
				case hostChan <- task:
				default:
					common.LogWarn("Failed to record task in history channel (full or closed)", map[string]interface{}{"task": task.Name, "host": hostname})
				}
			}

			currentClosure.HostContext.History.Store(task.Name, result.Output)

			logData := map[string]any{
				"host":     hostname,
				"task":     task.Name,
				"duration": result.Duration.String(),
				"status":   result.Status.String(),
			}

			var ignoredErrWrapper *pkg.IgnoredTaskError
			isIgnoredError := errors.As(result.Error, &ignoredErrWrapper)

			if isIgnoredError {
				originalErr := ignoredErrWrapper.Unwrap()
				logData["ignored"] = true
				logData["error_original"] = originalErr.Error()
				if result.Output != nil {
					logData["output"] = result.Output.String()
				}
				recapStats[hostname]["ignored"]++
				if result.Failed {
					recapStats[hostname]["failed"]++
				}

				if cfg.Logging.Format == "plain" {
					fmt.Printf("failed: [%s] => (ignored error: %v)\n", hostname, originalErr)
					PPrintOutput(result.Output, originalErr)
				} else {
					common.LogWarn("Task failed (ignored)", logData)
				}
			} else if result.Error != nil {
				levelHardErrored = true
				logData["error"] = result.Error.Error()
				if result.Output != nil {
					logData["output"] = result.Output.String()
				}
				if result.Failed {
					recapStats[hostname]["failed"]++
				}

				if cfg.Logging.Format == "plain" {
					fmt.Printf("failed: [%s] => (%v)\n", hostname, result.Error)
					PPrintOutput(result.Output, result.Error)
				} else {
					common.LogError("Task failed", logData)
				}
			} else {
				if result.Status == pkg.TaskStatusChanged {
					logData["changed"] = true
					if result.Output != nil {
						logData["output"] = result.Output.String()
					}
					recapStats[hostname]["changed"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("changed: [%s] => \n%v\n", hostname, result.Output)
					} else {
						common.LogInfo("Task changed", logData)
					}
				} else if result.Status == pkg.TaskStatusSkipped {
					recapStats[hostname]["skipped"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("skipped: [%s]\n", hostname)
					} else {
						common.LogInfo("Task skipped", logData)
					}
				} else {
					logData["changed"] = false
					if result.Output != nil {
						logData["output"] = result.Output.String()
					}
					recapStats[hostname]["ok"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("ok: [%s]\n", hostname)
					} else {
						common.LogInfo("Task ok", logData)
					}
				}
			}

			if cfg.ExecutionMode == "sequential" && levelHardErrored {
				common.LogWarn("Sequential mode: task hard-failed, stopping further result processing for this level.", map[string]interface{}{"task": task.Name, "host": hostname, "level": executionLevel})
				goto endProcessingLoop
			}

		case dispatchErr := <-errCh:
			if dispatchErr != nil {
				common.LogError("Critical dispatch error received while processing results", map[string]interface{}{"level": executionLevel, "error": dispatchErr})
				return true, fmt.Errorf("task dispatch/loading critical error on level %d: %w", executionLevel, dispatchErr)
			}

		case <-ctx.Done():
			common.LogWarn("Context cancelled while processing results for level.", map[string]interface{}{"level": executionLevel, "error": ctx.Err()})
			return true, fmt.Errorf("execution cancelled while processing results for level %d: %w", executionLevel, ctx.Err())
		}
	}
endProcessingLoop:

	if !levelHardErrored && ctx.Err() == nil && resultsReceived != numExpectedResultsOnLevel {
		select {
		case dispatchErr := <-errCh:
			if dispatchErr != nil {
				return true, fmt.Errorf("final dispatch error check on level %d: %w. Expected %d, got %d.", executionLevel, dispatchErr, numExpectedResultsOnLevel, resultsReceived)
			}
		default:
		}
		common.LogWarn("Mismatch in expected vs received results", map[string]interface{}{
			"level":    executionLevel,
			"expected": numExpectedResultsOnLevel,
			"received": resultsReceived,
		})
	}

	return levelHardErrored, nil
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
