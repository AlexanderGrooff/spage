package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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
			firstHostCtx, firstHostName := GetFirstAvailableHost(hostContexts)
			if firstHostCtx == nil {
				errMsg := fmt.Errorf("no hosts available for run_once task '%s'", task.Name)
				common.LogError("No hosts available for run_once task", map[string]interface{}{"error": errMsg})
				select {
				case errCh <- errMsg:
				case <-ctx.Done():
				}
				return // No hosts, so we can't proceed.
			}

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

			if len(closures) == 0 {
				// Empty loop, do nothing and continue to the next task.
				continue
			}

			if len(closures) > 1 {
				// Looped run_once: execute all loop items on the first host and aggregate results.
				var itemResults []pkg.TaskResult
				var finalError error
				changed := false
				var totalDuration time.Duration

				common.LogInfo("Executing run_once task with loop", map[string]interface{}{
					"task":           task.Name,
					"execution_host": firstHostName,
					"item_count":     len(closures),
				})

				for _, closure := range closures {
					// Note: delegate_to inside a run_once loop has complex behavior.
					// This implementation executes on the `firstHostCtx`'s designated runner.
					// A more advanced version might need to resolve delegation for each item.
					result := e.Runner.ExecuteTask(ctx, task, closure, cfg)
					itemResults = append(itemResults, result)
					totalDuration += result.Duration
					if result.Status == pkg.TaskStatusChanged {
						changed = true
					}
					if result.Error != nil {
						finalError = result.Error // Capture the first error and stop.
						break
					}
				}

				var itemOutputs []pkg.ModuleOutput
				for _, res := range itemResults {
					itemOutputs = append(itemOutputs, res.Output)
				}

				finalOutput := GenericOutput{
					"results": itemOutputs,
					"changed": changed,
				}
				finalStatus := pkg.TaskStatusOk
				if changed {
					finalStatus = pkg.TaskStatusChanged
				}
				if finalError != nil {
					finalStatus = pkg.TaskStatusFailed
				}

				finalClosure := pkg.ConstructClosure(firstHostCtx, task)
				aggregatedResult := pkg.TaskResult{
					Task:     task,
					Closure:  finalClosure,
					Error:    finalError,
					Status:   finalStatus,
					Failed:   finalError != nil,
					Output:   finalOutput,
					Duration: totalDuration,
				}

				// Propagate the aggregated facts to all hosts
				if task.Register != "" && aggregatedResult.Output != nil {
					if facts, ok := aggregatedResult.Output.(interface{ Facts() map[string]interface{} }); ok {
						factsToRegister := facts.Facts()
						common.LogDebug("Propagating run_once facts to all hosts", map[string]interface{}{
							"task":     task.Name,
							"variable": task.Register,
						})
						for _, hc := range hostContexts {
							hc.Facts.Store(task.Register, factsToRegister)
						}
					}
				}

				allResults := CreateRunOnceResultsForAllHosts(aggregatedResult, hostContexts, firstHostName)
				for _, result := range allResults {
					select {
					case resultsCh <- result:
					case <-ctx.Done():
						return
					}
				}
			} else {
				// Single run_once (no loop or loop with one item)
				closure := closures[0]

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
						closure.HostContext = delegatedHostContext
					}
				}

				common.LogInfo("Executing run_once task", map[string]interface{}{
					"task":           task.Name,
					"execution_host": closure.HostContext.Host.Name,
				})

				originalResult := e.Runner.ExecuteTask(ctx, task, closure, cfg)
				allResults := CreateRunOnceResultsForAllHosts(originalResult, hostContexts, firstHostName)
				for _, result := range allResults {
					select {
					case resultsCh <- result:
					case <-ctx.Done():
						return
					}
				}
			}

			// Continue to the next task definition in the level
			continue
		}

		// Normal task execution for non-run_once tasks
		for hostName, hostCtx := range hostContexts {

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

			for _, closure := range closures {
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
