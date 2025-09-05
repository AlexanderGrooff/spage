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
				task, ok := taskNode.(*pkg.Task)
				if !ok {
					// Handle meta tasks (like blocks) - they don't need revert
					if _, isMetaTask := taskNode.(*pkg.MetaTask); isMetaTask {
						common.LogDebug("Skipping meta task in Revert (no revert needed)", map[string]interface{}{"node": taskNode.String()})
						continue
					}
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

func (e *LocalGraphExecutor) loadTaskForHost(
	task pkg.GraphNode, hostContext *pkg.HostContext, hostContexts map[string]*pkg.HostContext,
	cfg *config.Config, errCh chan error, ctx context.Context, wg *sync.WaitGroup,
	resultsCh chan pkg.TaskResult) chan pkg.TaskResult {
	isParallelDispatch := cfg.ExecutionMode == "parallel"
	closures, err := GetTaskClosures(task, hostContext, cfg)
	if err != nil {
		errMsg := fmt.Errorf("critical error: failed to get task closures for task '%s' on host '%s': %w, aborting level", task.Params().Name, hostContext.Host.Name, err)
		common.LogError("Dispatch error in loadLevelTasks", map[string]interface{}{"error": errMsg})
		select {
		case errCh <- errMsg:
		case <-ctx.Done():
		}
		return make(chan pkg.TaskResult)
	}

	if len(closures) == 0 {
		return make(chan pkg.TaskResult)
	}

	for _, closure := range closures {
		// Resolve delegate_to if specified
		if task.Params().DelegateTo != "" {
			delegatedHostContext, err := GetDelegatedHostContext(task, hostContexts, closure, cfg)
			if err != nil {
				errMsg := fmt.Errorf("failed to resolve delegate_to for task '%s': %w", task.Params().Name, err)
				common.LogError("Delegate resolution error in loadLevelTasks", map[string]interface{}{"error": errMsg})
				select {
				case errCh <- errMsg:
				case <-ctx.Done():
				}
				return make(chan pkg.TaskResult)
			}
			if delegatedHostContext != nil {
				// Update the closure to use the delegated host context
				closure.HostContext = delegatedHostContext
			}
		}

		select {
		case <-ctx.Done():
			common.LogWarn("Context cancelled, stopping task dispatch for level.", map[string]interface{}{"task": task.Params().Name, "host": hostContext.Host.Name})
			select {
			case errCh <- fmt.Errorf("dispatch loop cancelled for task '%s' on host '%s': %w", task.Params().Name, hostContext.Host.Name, ctx.Err()):
			default:
			}
			return make(chan pkg.TaskResult)
		default:
		}

		if isParallelDispatch {
			// In parallel mode, we need to return a channel that the caller can read from
			// We'll create a local channel and start a goroutine to forward results
			localCh := make(chan pkg.TaskResult, 1)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer close(localCh)
				common.LogDebug("Starting task execution in parallel mode", map[string]interface{}{
					"task": task.Params().Name,
					"host": closure.HostContext.Host.Name,
				})
				select {
				case <-ctx.Done():
					common.LogDebug("Context cancelled before task execution", map[string]interface{}{
						"task": task.Params().Name,
						"host": closure.HostContext.Host.Name,
					})
					return
				default:
					taskResultCh := e.Runner.ExecuteTask(ctx, task, closure, cfg)
					common.LogDebug("Task execution started, waiting for results", map[string]interface{}{
						"task": task.Params().Name,
						"host": closure.HostContext.Host.Name,
					})

					// Check if this is a meta task that might produce multiple results
					if _, isMetaTask := task.(*pkg.MetaTask); isMetaTask {
						// Consume all results from meta task
						for result := range taskResultCh {
							common.LogDebug("Received meta task result", map[string]interface{}{
								"task":   task.Params().Name,
								"host":   closure.HostContext.Host.Name,
								"status": result.Status,
							})
							select {
							case localCh <- result:
								common.LogDebug("Meta task result sent to local channel", map[string]interface{}{
									"task": task.Params().Name,
									"host": closure.HostContext.Host.Name,
								})
							case <-ctx.Done():
								return
							}
						}
					} else {
						// Regular task - consume only one result
						common.LogDebug("Waiting for regular task result", map[string]interface{}{
							"task": task.Params().Name,
							"host": closure.HostContext.Host.Name,
						})
						select {
						case result := <-taskResultCh:
							common.LogDebug("Received regular task result", map[string]interface{}{
								"task":   task.Params().Name,
								"host":   closure.HostContext.Host.Name,
								"status": result.Status,
							})
							select {
							case localCh <- result:
								common.LogDebug("Regular task result sent to local channel", map[string]interface{}{
									"task": task.Params().Name,
									"host": closure.HostContext.Host.Name,
								})
							case <-ctx.Done():
								return
							}
						case <-ctx.Done():
							common.LogDebug("Context cancelled while waiting for task result", map[string]interface{}{
								"task": task.Params().Name,
								"host": closure.HostContext.Host.Name,
							})
							return
						}
					}
				}
			}()
			return localCh
		} else {
			taskResultCh := e.Runner.ExecuteTask(ctx, task, closure, cfg)

			// Check if this is a meta task that might produce multiple results
			if _, isMetaTask := task.(*pkg.MetaTask); isMetaTask {
				// Consume all results from meta task
				for result := range taskResultCh {
					select {
					case resultsCh <- result:
					case <-ctx.Done():
						return resultsCh
					}
				}
			} else {
				// Regular task - consume only one result
				select {
				case resultsCh <- <-taskResultCh:
				case <-ctx.Done():
					return resultsCh
				}
			}
		}
	}
	return resultsCh
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
	defer close(resultsCh)

	for _, taskDefinition := range tasksInLevel {
		task := taskDefinition

		// Handle run_once tasks separately
		// TODO: template the run_once condition with actual closure
		// TODO: use loadTaskForHost for run_once tasks
		if !task.Params().RunOnce.IsEmpty() && task.Params().RunOnce.IsTruthy(nil) {
			firstHostCtx, firstHostName := GetFirstAvailableHost(hostContexts)
			if firstHostCtx == nil {
				errMsg := fmt.Errorf("no hosts available for run_once task '%s'", task.Params().Name)
				common.LogError("No hosts available for run_once task", map[string]interface{}{"error": errMsg})
				select {
				case errCh <- errMsg:
				case <-ctx.Done():
				}
				return // No hosts, so we can't proceed.
			}

			closures, err := GetTaskClosures(task, firstHostCtx, cfg)
			if err != nil {
				errMsg := fmt.Errorf("critical error: failed to get task closures for run_once task '%s' on host '%s': %w", task.Params().Name, firstHostName, err)
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
					"task":           task.Params().Name,
					"execution_host": firstHostName,
					"item_count":     len(closures),
				})

				for _, closure := range closures {
					// Note: delegate_to inside a run_once loop has complex behavior.
					// This implementation executes on the `firstHostCtx`'s designated runner.
					// A more advanced version might need to resolve delegation for each item.

					// Handle meta tasks vs regular tasks differently
					resultCh := e.Runner.ExecuteTask(ctx, task, closure, cfg)

					var result pkg.TaskResult
					if _, isMetaTask := task.(*pkg.MetaTask); isMetaTask {
						// Meta task might produce multiple results - collect them all
						var results []pkg.TaskResult
						for res := range resultCh {
							results = append(results, res)
						}
						// For run_once, we'll use the first result as the primary result
						// TODO: Consider how to handle multiple results from meta tasks in run_once scenarios
						if len(results) > 0 {
							result = results[0]
							itemResults = append(itemResults, result)
							// Send additional results to the main channel
							for i := 1; i < len(results); i++ {
								select {
								case resultsCh <- results[i]:
								case <-ctx.Done():
									return
								}
							}
						} else {
							// Create a default result if no results
							result = pkg.TaskResult{
								Task:    task,
								Closure: closure,
								Status:  pkg.TaskStatusSkipped,
							}
							itemResults = append(itemResults, result)
						}
					} else {
						// Regular task - single result
						result = <-resultCh
						itemResults = append(itemResults, result)
					}
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

				finalClosure := task.ConstructClosure(firstHostCtx, cfg)
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
				if task.Params().Register != "" {
					reg := BuildRegisteredFromOutput(aggregatedResult.Output, aggregatedResult.Status == pkg.TaskStatusChanged || aggregatedResult.Changed)
					PropagateRegisteredToAllHosts(task, reg, hostContexts, nil)
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

				if task.Params().DelegateTo != "" {
					delegatedHostContext, err := GetDelegatedHostContext(task, hostContexts, closure, cfg)
					if err != nil {
						errMsg := fmt.Errorf("failed to resolve delegate_to for run_once task '%s': %w", task.Params().Name, err)
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
					"task":           task.Params().Name,
					"execution_host": closure.HostContext.Host.Name,
				})

				// Handle meta tasks vs regular tasks differently
				originalResultCh := e.Runner.ExecuteTask(ctx, task, closure, cfg)

				var originalResult pkg.TaskResult
				if _, isMetaTask := task.(*pkg.MetaTask); isMetaTask {
					// Meta task might produce multiple results - collect them all
					var results []pkg.TaskResult
					for result := range originalResultCh {
						results = append(results, result)
					}
					// For run_once, we'll use the first result as the primary result
					if len(results) > 0 {
						originalResult = results[0]
						// Send additional results to the main channel
						for i := 1; i < len(results); i++ {
							select {
							case resultsCh <- results[i]:
							case <-ctx.Done():
								return
							}
						}
					} else {
						// No results - create a default result
						originalResult = pkg.TaskResult{
							Task:    task,
							Closure: closure,
							Status:  pkg.TaskStatusSkipped,
						}
					}
				} else {
					// Regular task - single result
					originalResult = <-originalResultCh
				}

				// Propagate the registered variable from the single run_once execution to all hosts,
				// mirroring the behavior implemented for looped run_once above
				if task.Params().Register != "" {
					reg := BuildRegisteredFromOutput(originalResult.Output, originalResult.Status == pkg.TaskStatusChanged || originalResult.Changed)
					PropagateRegisteredToAllHosts(task, reg, hostContexts, nil)
				}

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
		for _, hostCtx := range hostContexts {
			// Check if this is a meta task (like block)
			if _, isMetaTask := task.(*pkg.MetaTask); isMetaTask {
				// Handle meta tasks synchronously to avoid race conditions
				taskResultCh := e.loadTaskForHost(task, hostCtx, hostContexts, cfg, errCh, ctx, wg, resultsCh)

				// Collect all results from the meta task synchronously
				for result := range taskResultCh {
					select {
					case resultsCh <- result:
					case <-ctx.Done():
						return
					}
				}
			} else {
				// Regular tasks can use the async forwarding approach
				taskResultCh := e.loadTaskForHost(task, hostCtx, hostContexts, cfg, errCh, ctx, wg, resultsCh)

				// Forward results from the task channel to the level results channel
				go func() {
					common.LogDebug("Starting result forwarding goroutine", map[string]interface{}{
						"task": task.Params().Name,
						"host": hostCtx.Host.Name,
					})
					for result := range taskResultCh {
						common.LogDebug("Forwarding result", map[string]interface{}{
							"task":   task.Params().Name,
							"host":   hostCtx.Host.Name,
							"status": result.Status,
						})
						select {
						case resultsCh <- result:
							common.LogDebug("Result forwarded successfully", map[string]interface{}{
								"task": task.Params().Name,
								"host": hostCtx.Host.Name,
							})
						case <-ctx.Done():
							common.LogDebug("Context cancelled, stopping result forwarding", map[string]interface{}{
								"task": task.Params().Name,
								"host": hostCtx.Host.Name,
							})
							return
						}
					}
					common.LogDebug("Result forwarding goroutine finished", map[string]interface{}{
						"task": task.Params().Name,
						"host": hostCtx.Host.Name,
					})
				}()
			}
		}
	}
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
