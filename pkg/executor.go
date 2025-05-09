package pkg

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
)

// TaskRunner defines an interface for how a single task is executed.
// This allows the core execution logic to be generic, while the actual
// task dispatch (local, Temporal activity, etc.) can be specific.
type TaskRunner interface {
	RunTask(ctx context.Context, task Task, closure *Closure, cfg *config.Config) TaskResult
}

// GraphExecutor defines the interface for running a Spage graph.
type GraphExecutor interface {
	Execute(ctx context.Context, cfg *config.Config, graph Graph, inventoryFile string) error
}

// BaseExecutor provides common logic for executing a Spage graph.
// It relies on a TaskRunner to perform the actual execution of individual tasks.
type BaseExecutor struct {
	Runner TaskRunner
}

// NewBaseExecutor creates a new BaseExecutor with the given TaskRunner.
func NewBaseExecutor(runner TaskRunner) *BaseExecutor {
	return &BaseExecutor{Runner: runner}
}

// Execute implements the main execution loop for a Spage graph.
func (e *BaseExecutor) Execute(ctx context.Context, cfg *config.Config, graph Graph, inventoryFile string) error {
	inventory, playTarget, err := e.loadInventory(inventoryFile)
	if err != nil {
		return err
	}

	if err := graph.CheckInventoryForRequiredInputs(inventory); err != nil {
		return fmt.Errorf("failed to check inventory for required inputs: %w", err)
	}

	hostContexts, err := e.getHostContexts(inventory)
	if err != nil {
		return err
	}
	defer func() {
		for _, hc := range hostContexts {
			hc.Close()
		}
	}()

	if cfg.Logging.Format == "plain" {
		fmt.Printf("\nPLAY [%s] ****************************************************\n", playTarget)
	} else {
		common.LogInfo("Starting play", map[string]interface{}{"target": playTarget})
	}

	recapStats := e.initializeRecapStats(hostContexts)
	var executionHistory []map[string]chan Task // For revert functionality

	orderedGraph, err := e.getOrderedGraph(cfg, graph)
	if err != nil {
		return err
	}

	for executionLevel, tasksInLevel := range orderedGraph {
		select {
		case <-ctx.Done():
			return fmt.Errorf("execution cancelled before level %d: %w", executionLevel, ctx.Err())
		default:
		}

		levelHistoryForRevert, numExpectedResultsOnLevel := e.prepareLevelHistoryAndGetCount(tasksInLevel, hostContexts, executionLevel)
		executionHistory = append(executionHistory, levelHistoryForRevert)

		resultsCh := make(chan TaskResult, numExpectedResultsOnLevel)
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
			if errRevert := RevertTasksWithConfig(executionHistory, hostContexts, cfg); errRevert != nil {
				return fmt.Errorf("run failed on level %d and also failed during revert: %w (original error trigger) | %v (revert error)", executionLevel, errors.New("task failure"), errRevert)
			}
			return fmt.Errorf("run failed on level %d and tasks reverted", executionLevel)
		}
	}

	e.printPlayRecap(cfg, recapStats)
	common.LogInfo("Play finished successfully.", map[string]interface{}{"target": playTarget})
	return nil
}

func (e *BaseExecutor) loadInventory(inventoryFile string) (*Inventory, string, error) {
	var inventory *Inventory
	var errI error
	playTarget := "localhost"
	if inventoryFile == "" {
		common.LogDebug("No inventory file specified, assuming target is this machine", nil)
		inventory = &Inventory{
			Hosts: map[string]*Host{
				"localhost": {Name: "localhost", IsLocal: true, Host: "localhost"},
			},
		}
	} else {
		playTarget = inventoryFile
		inventory, errI = LoadInventory(inventoryFile)
		if errI != nil {
			return nil, "", fmt.Errorf("failed to load inventory '%s': %w", inventoryFile, errI)
		}
	}
	return inventory, playTarget, nil
}

func (e *BaseExecutor) getHostContexts(inventory *Inventory) (map[string]*HostContext, error) {
	contexts, err := inventory.GetContextForRun()
	if err != nil {
		return nil, fmt.Errorf("failed to get host contexts for run: %w", err)
	}
	return contexts, nil
}

func (e *BaseExecutor) initializeRecapStats(hostContexts map[string]*HostContext) map[string]map[string]int {
	recapStats := make(map[string]map[string]int)
	for hostname := range hostContexts {
		recapStats[hostname] = map[string]int{"ok": 0, "changed": 0, "failed": 0, "skipped": 0, "ignored": 0}
	}
	return recapStats
}

func (e *BaseExecutor) getOrderedGraph(cfg *config.Config, graph Graph) ([][]Task, error) {
	if cfg.ExecutionMode == "parallel" {
		return graph.ParallelTasks(), nil
	} else if cfg.ExecutionMode == "sequential" {
		return graph.SequentialTasks(), nil
	}
	return nil, fmt.Errorf("unknown or unsupported execution mode: %s", cfg.ExecutionMode)
}

func (e *BaseExecutor) prepareLevelHistoryAndGetCount(
	tasksInLevel []Task,
	hostContexts map[string]*HostContext,
	executionLevel int,
) (map[string]chan Task, int) {
	levelHistoryForRevert := make(map[string]chan Task)
	numExpectedResultsOnLevel := 0

	for hostname, hc := range hostContexts {
		numTasksForHostOnLevel := 0
		for _, task := range tasksInLevel {
			closures, err := getTaskClosures(task, hc)
			if err != nil {
				common.LogError("Failed to get task closures for count, assuming 1", map[string]interface{}{
					"task": task.Name, "host": hostname, "level": executionLevel, "error": err,
				})
				numTasksForHostOnLevel++
			} else {
				numTasksForHostOnLevel += len(closures)
			}
		}
		levelHistoryForRevert[hostname] = make(chan Task, numTasksForHostOnLevel)
		numExpectedResultsOnLevel += numTasksForHostOnLevel
		common.DebugOutput("Expecting %d task instances for host '%s' on level %d", numTasksForHostOnLevel, hostname, executionLevel)
	}
	return levelHistoryForRevert, numExpectedResultsOnLevel
}

func (e *BaseExecutor) loadLevelTasks(
	ctx context.Context,
	tasksInLevel []Task,
	hostContexts map[string]*HostContext,
	resultsCh chan TaskResult,
	errCh chan error,
	cfg *config.Config,
) {
	defer close(resultsCh)

	var wg sync.WaitGroup
	isParallelDispatch := cfg.ExecutionMode == "parallel"

	for _, taskDefinition := range tasksInLevel {
		for hostNameKey, hostCtxInstance := range hostContexts {
			task := taskDefinition
			hostCtx := hostCtxInstance
			hostName := hostNameKey

			closures, err := getTaskClosures(task, hostCtx)
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
							taskResult := e.Runner.RunTask(ctx, task, closure, cfg)
							select {
							case resultsCh <- taskResult:
							case <-ctx.Done():
							}
						}
					}()
				} else {
					taskResult := e.Runner.RunTask(ctx, task, closure, cfg)
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

func (e *BaseExecutor) processLevelResults(
	ctx context.Context,
	resultsCh chan TaskResult,
	errCh chan error,
	recapStats map[string]map[string]int,
	executionHistory []map[string]chan Task,
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

			var ignoredErrWrapper *IgnoredTaskError
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
				if result.Status == TaskStatusChanged {
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
				} else if result.Status == TaskStatusSkipped {
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

func (e *BaseExecutor) printPlayRecap(cfg *config.Config, recapStats map[string]map[string]int) {
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
