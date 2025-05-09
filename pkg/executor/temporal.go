package executor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/AlexanderGrooff/spage/pkg"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/google/uuid"
)

// SpageActivityInput defines the input for our generic Spage task activity.
type SpageActivityInput struct {
	TaskDefinition   pkg.Task
	TargetHost       pkg.Host
	LoopItem         interface{} // nil if not a loop task or for the main item
	CurrentHostFacts map[string]interface{}
	SpageCoreConfig  *config.Config // Pass necessary config parts
}

// SpageActivityResult defines the output from our generic Spage task activity.
type SpageActivityResult struct {
	HostName          string
	TaskName          string
	Output            string
	Changed           bool
	Error             string // Store error message if any
	Skipped           bool
	Ignored           bool
	RegisteredVars    map[string]interface{}
	HostFactsSnapshot map[string]interface{}
}

// ExecuteSpageTaskActivity is the generic activity that runs a Spage task.
func ExecuteSpageTaskActivity(ctx context.Context, input SpageActivityInput) (*SpageActivityResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("ExecuteSpageTaskActivity started", "task", input.TaskDefinition.Name, "host", input.TargetHost.Name)
	activity.RecordHeartbeat(ctx, fmt.Sprintf("Starting task %s on host %s", input.TaskDefinition.Name, input.TargetHost.Name))

	hostCtx, err := pkg.InitializeHostContext(&input.TargetHost)
	if err != nil {
		logger.Error("Failed to initialize host context", "host", input.TargetHost.Name, "task", input.TaskDefinition.Name, "error", err)
		return &SpageActivityResult{
			HostName: input.TargetHost.Name,
			TaskName: input.TaskDefinition.Name,
			Error:    fmt.Sprintf("failed to initialize host context for task %s: %v", input.TaskDefinition.Name, err),
		}, nil
	}
	defer hostCtx.Close()

	// Load facts from workflow (these are now pre-processed by GetInitialFactsForHost)
	if input.CurrentHostFacts != nil {
		for k, v := range input.CurrentHostFacts {
			hostCtx.Facts.Store(k, v)
		}
	}
	// The direct merge of input.TargetHost.Vars is removed as GetInitialFactsForHost now handles this layering.

	closure := pkg.ConstructClosure(hostCtx, input.TaskDefinition) // Assumes ConstructClosure is in package pkg

	if input.LoopItem != nil {
		loopVarName := "item"
		// if input.TaskDefinition.LoopControl.LoopVar != "" { // Temporarily commented out
		// 	loopVarName = input.TaskDefinition.LoopControl.LoopVar
		// }
		closure.ExtraFacts[loopVarName] = input.LoopItem
		logger.Debug("Loop item added to closure facts", "loopVar", loopVarName, "value", input.LoopItem)
	}

	taskResult := input.TaskDefinition.ExecuteModule(closure)
	activity.RecordHeartbeat(ctx, fmt.Sprintf("Finished task %s on host %s", input.TaskDefinition.Name, input.TargetHost.Name))

	result := &SpageActivityResult{
		HostName:       input.TargetHost.Name,
		TaskName:       input.TaskDefinition.Name,
		RegisteredVars: make(map[string]interface{}),
	}

	var ignoredError *pkg.IgnoredTaskError // Assumes IgnoredTaskError is in package pkg
	if errors.As(taskResult.Error, &ignoredError) {
		result.Ignored = true
		originalErr := ignoredError.Unwrap()
		result.Error = originalErr.Error()
		logger.Warn("Task failed but error was ignored", "task", input.TaskDefinition.Name, "originalError", originalErr)
		failureMap := map[string]interface{}{
			"failed":  true,
			"changed": false,
			"msg":     originalErr.Error(),
			"ignored": true,
		}
		if taskResult.Output != nil {
			if factProvider, ok := taskResult.Output.(pkg.FactProvider); ok { // Assumes FactProvider is in package pkg
				outputFacts := factProvider.AsFacts()
				for k, v := range outputFacts {
					failureMap[k] = v
				}
			}
		}
		if input.TaskDefinition.Register != "" {
			result.RegisteredVars[input.TaskDefinition.Register] = failureMap
		}
	} else if taskResult.Error != nil {
		result.Error = taskResult.Error.Error()
		logger.Error("Task execution failed", "task", input.TaskDefinition.Name, "error", taskResult.Error)
		failureMap := map[string]interface{}{
			"failed":  true,
			"changed": false,
			"msg":     taskResult.Error.Error(),
		}
		if taskResult.Output != nil {
			if factProvider, ok := taskResult.Output.(pkg.FactProvider); ok {
				outputFacts := factProvider.AsFacts()
				for k, v := range outputFacts {
					failureMap[k] = v
				}
			}
		}
		if input.TaskDefinition.Register != "" {
			result.RegisteredVars[input.TaskDefinition.Register] = failureMap
		}
	} else {
		if taskResult.Output != nil {
			result.Output = taskResult.Output.String()
			result.Changed = taskResult.Output.Changed()
			logger.Info("Task executed successfully", "task", input.TaskDefinition.Name, "changed", result.Changed)
			if input.TaskDefinition.Register != "" {
				valueToStore := pkg.ConvertOutputToFactsMap(taskResult.Output) // Assumes ConvertOutputToFactsMap is in package pkg
				result.RegisteredVars[input.TaskDefinition.Register] = valueToStore
				logger.Info("Variable registered", "task", input.TaskDefinition.Name, "variable", input.TaskDefinition.Register)
			}
		} else {
			result.Skipped = true
			logger.Info("Task executed, no output (potentially skipped)", "task", input.TaskDefinition.Name)
			if input.TaskDefinition.Register != "" {
				result.RegisteredVars[input.TaskDefinition.Register] = map[string]interface{}{
					"failed":  false,
					"changed": false,
					"skipped": true,
					"ignored": false,
				}
			}
		}
	}

	// After module execution and handling 'register', capture all facts from the activity's HostContext.
	result.HostFactsSnapshot = make(map[string]interface{})
	hostCtx.Facts.Range(func(key, value interface{}) bool {
		if kStr, ok := key.(string); ok {
			result.HostFactsSnapshot[kStr] = value
		} else {
			// Log or handle non-string keys if necessary, though sync.Map keys are typically strings here.
			logger.Warn("Non-string key found in HostContext facts during snapshot", "key_type", fmt.Sprintf("%T", key), "key_value", key)
		}
		return true
	})

	return result, nil
}

// processActivityResultAndRegisterFacts handles the common logic for processing an activity's result,
// logging outcomes, and registering any variables into hostFacts.
// It returns an error if the task definitively failed and was not ignored.
func processActivityResultAndRegisterFacts(
	ctx workflow.Context,
	activityResult SpageActivityResult,
	taskName string,
	hostName string,
	hostFacts map[string]map[string]interface{},
) error {
	logger := workflow.GetLogger(ctx)
	if activityResult.Error != "" && !activityResult.Ignored {
		logger.Error("Task failed as reported by activity", "task", taskName, "host", hostName, "reportedError", activityResult.Error)
		return fmt.Errorf("task '%s' on host '%s' failed: %s", taskName, hostName, activityResult.Error)
	}
	if activityResult.Error != "" && activityResult.Ignored {
		logger.Warn("Task failed but was ignored", "task", taskName, "host", hostName, "reportedError", activityResult.Error)
	}

	if activityResult.Skipped {
		logger.Info("Task skipped", "task", taskName, "host", hostName)
	} else if !activityResult.Ignored {
		status := "ok"
		if activityResult.Changed {
			status = "changed"
		}
		logger.Info("Task completed", "task", taskName, "host", hostName, "status", status)
	}

	// Ensure host entry exists in the workflow's main hostFacts map
	if _, ok := hostFacts[activityResult.HostName]; !ok {
		logger.Warn("Host facts map not found for host, creating new one.", "host", activityResult.HostName)
		hostFacts[activityResult.HostName] = make(map[string]interface{})
	}

	// 1. Merge the full fact snapshot from the activity first.
	// This applies changes made directly by modules like set_fact.
	if activityResult.HostFactsSnapshot != nil {
		for key, value := range activityResult.HostFactsSnapshot {
			hostFacts[activityResult.HostName][key] = value
			logger.Debug("Fact updated/set from activity snapshot", "host", activityResult.HostName, "variable", key)
		}
	}

	// 2. Merge registered variables from activityResult.RegisteredVars.
	// This handles the 'register:' keyword and will overwrite snapshot values for the registered key.
	if len(activityResult.RegisteredVars) > 0 {
		for key, value := range activityResult.RegisteredVars {
			hostFacts[activityResult.HostName][key] = value
			logger.Info("Fact registered by workflow from activity result (register keyword)", "host", activityResult.HostName, "variable", key)
		}
	}
	return nil
}

// TemporalTaskRunner implements the TaskRunner interface for Temporal activity execution.
// It requires access to the workflow.Context to execute activities.
// Note: This runner is conceptual for showing how Temporal fits the TaskRunner pattern.
// The SpageTemporalWorkflow will still manage its own execution loop due to
// differences in fact/state management compared to LocalGraphExecutor's assumptions.
type TemporalTaskRunner struct {
	WorkflowCtx workflow.Context // The Temporal workflow context
}

// NewTemporalTaskRunner creates a new TemporalTaskRunner.
func NewTemporalTaskRunner(workflowCtx workflow.Context) *TemporalTaskRunner {
	return &TemporalTaskRunner{WorkflowCtx: workflowCtx}
}

// RunTask for Temporal dispatches the task as a Temporal activity.
// It converts the SpageActivityResult from the activity into a TaskResult.
// The original SpageActivityResult is stored in TaskResult.ExecutionSpecificOutput.
func (r *TemporalTaskRunner) ExecuteTask(execCtx workflow.Context, task pkg.Task, closure *pkg.Closure, cfg *config.Config) pkg.TaskResult {
	// execCtx is the context of the coroutine calling ExecuteTask.
	// r.WorkflowCtx is the root workflow context, kept for logger or other global workflow info if needed, but execCtx for blocking ops.
	logger := workflow.GetLogger(execCtx) // Use execCtx for logger too for better context association

	currentHostFacts := closure.GetFacts() // Facts from the closure, prepared by SpageTemporalWorkflow
	loopItem, _ := closure.ExtraFacts["item"]

	activityInput := SpageActivityInput{
		TaskDefinition:   task,
		TargetHost:       *closure.HostContext.Host,
		LoopItem:         loopItem,
		CurrentHostFacts: currentHostFacts,
		SpageCoreConfig:  cfg,
	}

	var activityOutput SpageActivityResult // Use a distinct name from the input

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	// Use execCtx for Temporal API calls like WithActivityOptions and ExecuteActivity
	temporalAwareCtx := workflow.WithActivityOptions(execCtx, ao)

	startTime := workflow.Now(execCtx) // Record start time before executing activity
	future := workflow.ExecuteActivity(temporalAwareCtx, ExecuteSpageTaskActivity, activityInput)
	errOnGet := future.Get(execCtx, &activityOutput) // Use execCtx for future.Get()
	endTime := workflow.Now(execCtx)                 // Calculate duration after activity completion or error
	duration := endTime.Sub(startTime)

	if errOnGet != nil {
		logger.Error("Temporal activity future.Get() failed", "task", task.Name, "host", closure.HostContext.Host.Name, "error", errOnGet)
		return pkg.TaskResult{
			Task:                    task,
			Closure:                 closure,
			Error:                   fmt.Errorf("activity %s on host %s failed to complete: %w", task.Name, closure.HostContext.Host.Name, errOnGet),
			Status:                  pkg.TaskStatusFailed,
			Failed:                  true,
			Duration:                duration,
			ExecutionSpecificOutput: nil, // No successful activityOutput to store
		}
	}

	var finalError error
	if activityOutput.Error != "" {
		if activityOutput.Ignored {
			finalError = &pkg.IgnoredTaskError{OriginalErr: errors.New(activityOutput.Error)}
		} else {
			finalError = errors.New(activityOutput.Error)
		}
	}

	finalStatus := pkg.TaskStatusOk
	if activityOutput.Skipped {
		finalStatus = pkg.TaskStatusSkipped
	} else if finalError != nil {
		if !activityOutput.Ignored { // Only set to Failed if not ignored
			finalStatus = pkg.TaskStatusFailed
		}
		// If ignored, status remains Ok or Changed (if applicable) unless explicitly set otherwise
	} else if activityOutput.Changed {
		finalStatus = pkg.TaskStatusChanged
	}

	// Note: TaskResult.Output (ModuleOutput) is not directly populated from SpageActivityResult.Output (string).
	// This would require parsing the string or changing SpageActivityResult.
	// For now, TaskResult.Output will be nil when using TemporalTaskRunner if ExecuteSpageTaskActivity doesn't provide it.

	return pkg.TaskResult{
		Task:                    task,
		Closure:                 closure,
		Error:                   finalError,
		Status:                  finalStatus,
		Failed:                  (finalError != nil && !activityOutput.Ignored), // True if a non-ignored error occurred
		Changed:                 activityOutput.Changed,
		Duration:                duration,
		ExecutionSpecificOutput: activityOutput, // Store the full SpageActivityResult
		// Output: nil, // ModuleOutput remains nil for now
	}
}

func (r *TemporalTaskRunner) RevertTask(execCtx workflow.Context, task pkg.Task, closure *pkg.Closure, cfg *config.Config) pkg.TaskResult {
	// execCtx is the context of the coroutine calling RevertTask.
	logger := workflow.GetLogger(execCtx) // Use execCtx for logger

	currentHostFacts := closure.GetFacts()
	loopItem, _ := closure.ExtraFacts["item"]

	activityInput := SpageActivityInput{
		TaskDefinition:   task,
		TargetHost:       *closure.HostContext.Host,
		LoopItem:         loopItem,
		CurrentHostFacts: currentHostFacts,
		SpageCoreConfig:  cfg,
	}

	var activityOutput SpageActivityResult // Use a distinct name from the input

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	// Use execCtx for Temporal API calls like WithActivityOptions and ExecuteActivity
	temporalAwareCtx := workflow.WithActivityOptions(execCtx, ao)

	startTime := workflow.Now(execCtx) // Record start time before executing activity
	future := workflow.ExecuteActivity(temporalAwareCtx, ExecuteSpageTaskActivity, activityInput)
	errOnGet := future.Get(execCtx, &activityOutput) // Use execCtx for future.Get()
	endTime := workflow.Now(execCtx)                 // Calculate duration after activity completion or error
	duration := endTime.Sub(startTime)

	if errOnGet != nil {
		logger.Error("Temporal activity future.Get() failed", "task", task.Name, "host", closure.HostContext.Host.Name, "error", errOnGet)
		return pkg.TaskResult{
			Task:                    task,
			Closure:                 closure,
			Error:                   fmt.Errorf("activity %s on host %s failed to complete: %w", task.Name, closure.HostContext.Host.Name, errOnGet),
			Status:                  pkg.TaskStatusFailed,
			Failed:                  true,
			Duration:                duration,
			ExecutionSpecificOutput: nil, // No successful activityOutput to store
		}
	}

	var finalError error
	if activityOutput.Error != "" {
		if activityOutput.Ignored {
			finalError = &pkg.IgnoredTaskError{OriginalErr: errors.New(activityOutput.Error)}
		} else {
			finalError = errors.New(activityOutput.Error)
		}
	}

	finalStatus := pkg.TaskStatusOk
	if activityOutput.Skipped {
		finalStatus = pkg.TaskStatusSkipped
	} else if finalError != nil {
		if !activityOutput.Ignored { // Only set to Failed if not ignored
			finalStatus = pkg.TaskStatusFailed
		}
		// If ignored, status remains Ok or Changed (if applicable) unless explicitly set otherwise
	} else if activityOutput.Changed {
		finalStatus = pkg.TaskStatusChanged
	}

	// Note: TaskResult.Output (ModuleOutput) is not directly populated from SpageActivityResult.Output (string).
	// This would require parsing the string or changing SpageActivityResult.
	// For now, TaskResult.Output will be nil when using TemporalTaskRunner if ExecuteSpageTaskActivity doesn't provide it.

	return pkg.TaskResult{
		Task:                    task,
		Closure:                 closure,
		Error:                   finalError,
		Status:                  finalStatus,
		Failed:                  (finalError != nil && !activityOutput.Ignored), // True if a non-ignored error occurred
		Changed:                 activityOutput.Changed,
		Duration:                duration,
		ExecutionSpecificOutput: activityOutput, // Store the full SpageActivityResult
		// Output: nil, // ModuleOutput remains nil for now
	}
}

type TemporalGraphExecutor struct {
	Runner TemporalTaskRunner
}

func NewTemporalGraphExecutor(runner TemporalTaskRunner) *TemporalGraphExecutor {
	return &TemporalGraphExecutor{Runner: runner}
}

func (e *TemporalGraphExecutor) loadLevelTasks(
	workflowCtx workflow.Context,
	tasksInLevel []pkg.Task,
	inventoryHosts map[string]*pkg.Host,
	workflowHostFacts map[string]map[string]interface{},
	resultsCh workflow.Channel,
	errCh workflow.Channel,
	cfg *config.Config,
) {
	logger := workflow.GetLogger(workflowCtx)
	logger.Info("loadLevelTasks started")
	defer logger.Info("loadLevelTasks finished")

	defer resultsCh.Close()
	defer errCh.Close()

	isParallelDispatch := cfg.ExecutionMode == "parallel"
	var completionCh workflow.Channel
	// Pre-calculate numDispatchedTasks for buffered channel capacity if in parallel mode
	actualDispatchedTasks := 0 // Renamed from numDispatchedTasks to avoid confusion in the pre-calculation loop

	if isParallelDispatch {
		countForBuffer := 0
		for _, taskDefinitionForCount := range tasksInLevel {
			for hostNameForCount, hostDefForCount := range inventoryHosts {
				tempHostCtxForCount := &pkg.HostContext{
					Host:  hostDefForCount,
					Facts: &sync.Map{},
				}
				if facts, ok := workflowHostFacts[hostNameForCount]; ok {
					for k, v := range facts {
						tempHostCtxForCount.Facts.Store(k, v)
					}
				}
				closuresForCount, err := pkg.GetTaskClosures(taskDefinitionForCount, tempHostCtxForCount)
				if err != nil {
					errMsg := fmt.Errorf("critical error during pre-count for task closures: task '%s' on host '%s': %w", taskDefinitionForCount.Name, hostNameForCount, err)
					common.LogError("Dispatch error in loadLevelTasks (pre-count)", map[string]interface{}{"error": errMsg})
					errCh.SendAsync(errMsg)
					return // Abort if counting fails
				}
				countForBuffer += len(closuresForCount)
			}
		}
		if countForBuffer > 0 {
			completionCh = workflow.NewBufferedChannel(workflowCtx, countForBuffer)
			logger.Debug("Parallel dispatch mode: completionCh created with buffer.", "capacity", countForBuffer)
		} else {
			// No tasks to dispatch in parallel, no need for completionCh
			logger.Debug("Parallel dispatch mode: No tasks to dispatch, completionCh not created.")
		}
	}

	for _, taskDefinition := range tasksInLevel {
		for hostName, hostDef := range inventoryHosts {
			task := taskDefinition
			tempHostCtx := &pkg.HostContext{
				Host:  hostDef,
				Facts: &sync.Map{},
			}
			if facts, ok := workflowHostFacts[hostName]; ok {
				for k, v := range facts {
					tempHostCtx.Facts.Store(k, v)
				}
			}

			closures, err := pkg.GetTaskClosures(task, tempHostCtx)
			if err != nil {
				errMsg := fmt.Errorf("critical error: failed to get task closures for task '%s' on host '%s': %w. Aborting level.", task.Name, hostName, err)
				common.LogError("Dispatch error in loadLevelTasks", map[string]interface{}{"error": errMsg})
				errCh.SendAsync(errMsg)
				return
			}

			for _, individualClosure := range closures {
				closure := individualClosure

				if isParallelDispatch {
					actualDispatchedTasks++ // Increment the counter for actual dispatches
					logger.Debug("Dispatching parallel task", "task", task.Name, "host", hostName, "closure_item", closure.ExtraFacts["item"])
					workflow.Go(workflowCtx, func(childTaskCtx workflow.Context) {
						logger.Debug("Parallel task coroutine started", "task", task.Name, "host", hostName)
						taskResult := e.Runner.ExecuteTask(childTaskCtx, task, closure, cfg)
						logger.Debug("Parallel task executed, sending to resultsCh", "task", task.Name, "host", hostName)
						resultsCh.SendAsync(taskResult)

						logger.Debug("Attempting to send to completionCh", "task", task.Name, "host", hostName)
						completionCh.SendAsync(true)
						logger.Debug("Successfully sent to completionCh", "task", task.Name, "host", hostName)
					})
				} else {
					logger.Debug("Executing sequential task", "task", task.Name, "host", hostName, "closure_item", closure.ExtraFacts["item"])
					taskResult := e.Runner.ExecuteTask(workflowCtx, task, closure, cfg)
					logger.Debug("Sequential task executed, sending to resultsCh", "task", task.Name, "host", hostName)
					resultsCh.SendAsync(taskResult)
					logger.Debug("Sequential task result sent to resultsCh", "task", task.Name, "host", hostName)
				}
			}
		}
	}

	if isParallelDispatch && actualDispatchedTasks > 0 { // Use actualDispatchedTasks here
		logger.Info("Waiting for parallel tasks to complete", "count", actualDispatchedTasks)
		for i := 0; i < actualDispatchedTasks; i++ { // Loop up to actualDispatchedTasks
			logger.Debug("Attempting to receive from completionCh", "iteration", i+1, "total_expected_signals", actualDispatchedTasks, "current_completed_signals_in_loop", i)
			// Ensure completionCh is not nil (it wouldn't be if actualDispatchedTasks > 0)
			if completionCh == nil {
				logger.Error("completionCh is nil before Receive, this should not happen if actualDispatchedTasks > 0")
				// This is a critical logic error, potentially send to errCh or panic workflow.
				// For now, let it proceed, it will panic on nil channel receive.
			}
			completionCh.Receive(workflowCtx, nil)
			logger.Debug("Successfully received from completionCh", "iteration", i+1, "completed_signals_in_loop", i+1)
		}
		logger.Info("All parallel tasks completed for this level.")
	}
	logger.Debug("loadLevelTasks: all tasks dispatched for the level.", "num_dispatched_parallel", actualDispatchedTasks)
}

func (e *TemporalGraphExecutor) processLevelResults(
	resultsCh workflow.Channel,
	errCh workflow.Channel,
	hostFacts map[string]map[string]interface{},
	executionLevel int,
	cfg *config.Config,
	numExpectedResultsOnLevel int,
) (bool, error) {
	ctx := e.Runner.WorkflowCtx
	logger := workflow.GetLogger(ctx)

	var levelHardErrored bool
	resultsReceived := 0
	var dispatchError error
	resultsChActive := true
	errChActive := true

	selector := workflow.NewSelector(ctx)

	selector.AddReceive(errCh, func(c workflow.ReceiveChannel, more bool) {
		logger.Debug("errCh handler invoked", "more", more, "current_dispatchError", dispatchError != nil)
		if !more {
			logger.Info("errCh closed by sender.")
			errChActive = false
			return
		}
		var errFromDispatch error
		c.Receive(ctx, &errFromDispatch)
		if errFromDispatch != nil {
			dispatchError = errFromDispatch
			logger.Error("Dispatch error received from loadLevelTasks", "level", executionLevel, "error", dispatchError)
			levelHardErrored = true
		}
	})

	selector.AddReceive(resultsCh, func(c workflow.ReceiveChannel, more bool) {
		logger.Debug("resultsCh handler invoked", "more", more, "current_resultsReceived", resultsReceived, "expected", numExpectedResultsOnLevel)
		if !more {
			logger.Info("resultsCh closed by sender.", "level", executionLevel, "results_received_so_far", resultsReceived)
			resultsChActive = false
			return
		}

		var result pkg.TaskResult
		c.Receive(ctx, &result)
		resultsReceived++

		if result.Closure == nil || result.Closure.HostContext == nil || result.Closure.HostContext.Host == nil {
			logger.Error("Received TaskResult with nil Closure/HostContext/Host", "level", executionLevel, "result_task_name", result.Task.Name)
			levelHardErrored = true
			if dispatchError == nil {
				dispatchError = fmt.Errorf("corrupted task result for task %s on level %d", result.Task.Name, executionLevel)
			}
			return
		}

		hostname := result.Closure.HostContext.Host.Name
		taskName := result.Task.Name

		activitySpecificOutput, ok := result.ExecutionSpecificOutput.(SpageActivityResult)
		if !ok {
			logger.Error("TaskResult.ExecutionSpecificOutput is not of type SpageActivityResult", "task", taskName, "host", hostname)
		} else {
			errFactProcessing := processActivityResultAndRegisterFacts(ctx, activitySpecificOutput, taskName, hostname, hostFacts)
			if errFactProcessing != nil {
				logger.Error("Task processing reported an error after fact registration", "task", taskName, "host", hostname, "error", errFactProcessing)
				levelHardErrored = true
			}
		}

		var ignoredErrWrapper *pkg.IgnoredTaskError
		isIgnoredError := errors.As(result.Error, &ignoredErrWrapper)

		if result.Error != nil && !isIgnoredError {
			logger.Error("Task failed (from TaskResult)", "level", executionLevel, "task", taskName, "host", hostname, "error", result.Error)
			levelHardErrored = true
		} else if result.Error != nil && isIgnoredError {
			logger.Warn("Task failed but was ignored (from TaskResult)", "level", executionLevel, "task", taskName, "host", hostname, "original_error", ignoredErrWrapper.Unwrap())
		} else {
			logger.Info("Task completed successfully or skipped (from TaskResult)", "level", executionLevel, "task", taskName, "host", hostname, "status", result.Status.String(), "changed", result.Changed)
		}
	})

	for (resultsChActive || errChActive) && resultsReceived < numExpectedResultsOnLevel && dispatchError == nil {
		selector.Select(ctx)
		logger.Debug("processLevelResults loop status",
			"level", executionLevel,
			"resultsChActive", resultsChActive, "errChActive", errChActive,
			"resultsReceived", resultsReceived, "numExpectedResultsOnLevel", numExpectedResultsOnLevel,
			"dispatchError", dispatchError != nil)
	}

	if dispatchError != nil {
		logger.Error("Level processing stopped due to error.", "level", executionLevel, "error", dispatchError, "results_received", resultsReceived)
		return true, dispatchError
	}

	if !resultsChActive && resultsReceived < numExpectedResultsOnLevel {
		errMsg := fmt.Errorf("level %d results channel closed prematurely: got %d, expected %d. loadLevelTasks might have an issue", executionLevel, resultsReceived, numExpectedResultsOnLevel)
		logger.Error(errMsg.Error())
		return true, errMsg
	}

	if resultsReceived < numExpectedResultsOnLevel {
		errMsg := fmt.Errorf("level %d did not receive all expected results: got %d, expected %d. resultsCh may have closed prematurely or loadLevelTasks did not send all results", executionLevel, resultsReceived, numExpectedResultsOnLevel)
		logger.Error(errMsg.Error())
		return true, errMsg
	}

	logger.Info("All expected results received for level.", "level", executionLevel, "count", resultsReceived)
	return levelHardErrored, nil
}

func (e *TemporalGraphExecutor) Execute(
	inventoryHosts map[string]*pkg.Host,
	workflowHostFacts map[string]map[string]interface{},
	orderedGraph [][]pkg.Task,
	cfg *config.Config,
) error {
	for executionLevel, tasksInLevel := range orderedGraph {
		// numExpectedResultsOnLevel needs to be calculated using workflowHostFacts for GetTaskClosures
		// This requires adapting PrepareLevelHistoryAndGetCount or similar logic.
		// For now, this calculation will be simplified or deferred.
		// Let's assume GetTaskClosures is adapted to use workflowHostFacts.

		// Simplified numExpectedResultsOnLevel calculation for now:
		var numExpectedResultsOnLevel int
		for _, task := range tasksInLevel {
			for _, hostDef := range inventoryHosts {
				// Construct a temporary, lightweight HostContext for GetTaskClosures
				tempHostCtxForCount := &pkg.HostContext{
					Host:  hostDef,
					Facts: &sync.Map{},
				}
				if facts, ok := workflowHostFacts[hostDef.Name]; ok {
					for k, v := range facts {
						tempHostCtxForCount.Facts.Store(k, v)
					}
				}

				closures, err := pkg.GetTaskClosures(task, tempHostCtxForCount)
				if err != nil {
					return fmt.Errorf("failed to get task closures for count on level %d, task %s, host %s: %w", executionLevel, task.Name, hostDef.Name, err)
				}
				numExpectedResultsOnLevel += len(closures)
			}
		}
		if numExpectedResultsOnLevel == 0 && len(tasksInLevel) > 0 {
			workflow.GetLogger(e.Runner.WorkflowCtx).Info("No task instances to execute for level, skipping.", "level", executionLevel)
			continue
		}

		resultsCh := workflow.NewBufferedChannel(e.Runner.WorkflowCtx, numExpectedResultsOnLevel)
		errCh := workflow.NewChannel(e.Runner.WorkflowCtx)

		workflow.Go(e.Runner.WorkflowCtx, func(ctx workflow.Context) {
			e.loadLevelTasks(ctx, tasksInLevel, inventoryHosts, workflowHostFacts, resultsCh, errCh, cfg)
		})

		levelErrored, errProcessingResults := e.processLevelResults(
			resultsCh, errCh,
			workflowHostFacts,
			executionLevel,
			cfg, numExpectedResultsOnLevel,
		)
		if errProcessingResults != nil {
			return errProcessingResults
		}

		if levelErrored {
			common.LogInfo("Run failed, task reversion required (Temporal Revert TBD)", map[string]interface{}{"level": executionLevel})
			return fmt.Errorf("run failed on level %d", executionLevel)
		}
	}
	return nil
}

// SpageTemporalWorkflow defines the main workflow logic.
func SpageTemporalWorkflow(ctx workflow.Context, graphInput *pkg.Graph, inventoryInput *pkg.Inventory, spageConfigInput *config.Config) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("SpageTemporalWorkflow started", "workflowId", workflow.GetInfo(ctx).WorkflowExecution.ID)

	temporalRunner := NewTemporalTaskRunner(ctx)
	e := TemporalGraphExecutor{Runner: *temporalRunner}

	workflowHostFacts := make(map[string]map[string]interface{})
	if inventoryInput != nil && inventoryInput.Hosts != nil {
		for hostName, hostDef := range inventoryInput.Hosts {
			hostFacts := make(map[string]interface{})
			if hostDef.Vars != nil {
				for k, v := range hostDef.Vars {
					hostFacts[k] = v
				}
			}
			hostFacts["ansible_hostname"] = hostDef.Name
			workflowHostFacts[hostName] = hostFacts
		}
	}
	if inventoryInput != nil {
		if allGroup, ok := inventoryInput.Groups["all"]; ok && allGroup.Vars != nil {
			for hostNameFromFacts := range workflowHostFacts {
				for k, v := range allGroup.Vars {
					if hostSpecificFacts, hostExists := workflowHostFacts[hostNameFromFacts]; hostExists {
						if _, varExists := hostSpecificFacts[k]; !varExists {
							hostSpecificFacts[k] = v
						}
					}
				}
			}
		}
		for groupName, groupDef := range inventoryInput.Groups {
			if groupName == "all" || groupDef.Vars == nil {
				continue
			}
			for _, hostMember := range groupDef.Hosts {
				hostMemberNameStr := hostMember.Name
				if hostSpecificFacts, hostExists := workflowHostFacts[hostMemberNameStr]; hostExists {
					for k, v := range groupDef.Vars {
						if _, varExists := hostSpecificFacts[k]; !varExists {
							hostSpecificFacts[k] = v
						}
					}
				} else {
					logger.Warn("Host listed in group not found in main host definitions during fact merging", "group", groupName, "host", hostMemberNameStr)
				}
			}
		}
	}

	orderedGraph, err := pkg.GetOrderedGraph(spageConfigInput, *graphInput)
	if err != nil {
		logger.Error("Failed to get ordered graph", "error", err)
		return err
	}

	inventoryHostsMap := make(map[string]*pkg.Host)
	if inventoryInput != nil && inventoryInput.Hosts != nil {
		inventoryHostsMap = inventoryInput.Hosts
	}

	err = e.Execute(inventoryHostsMap, workflowHostFacts, orderedGraph, spageConfigInput)

	if err != nil {
		logger.Error("SpageTemporalWorkflow failed", "error", err)
		return err
	}

	logger.Info("SpageTemporalWorkflow completed successfully.")
	return nil
}

// RunSpageTemporalWorkerAndWorkflowOptions defines options for RunSpageTemporalWorkerAndWorkflow.
type RunSpageTemporalWorkerAndWorkflowOptions struct {
	Graph         *pkg.Graph
	InventoryPath string
	LoadedConfig  *config.Config // Changed from ConfigPath to break import cycle with cmd
}

// RunSpageTemporalWorkerAndWorkflow sets up and runs a Temporal worker for Spage tasks,
// and can optionally trigger a workflow execution.
func RunSpageTemporalWorkerAndWorkflow(opts RunSpageTemporalWorkerAndWorkflowOptions) {
	log.Println("Starting Spage Temporal application runner...")

	spageAppConfig := opts.LoadedConfig
	if spageAppConfig == nil {
		log.Println("Warning: No Spage configuration provided to RunSpageTemporalWorkerAndWorkflow. Using a default config.")
		spageAppConfig = &config.Config{
			ExecutionMode: "parallel",
			Logging:       config.LoggingConfig{Format: "plain", Level: "info"},
			Temporal: config.TemporalConfig{
				Address:          "",
				TaskQueue:        "SPAGE_DEFAULT_TASK_QUEUE",
				WorkflowIDPrefix: "spage-workflow",
			},
		}
	} else {
		log.Println("Using provided Spage configuration.")
	}

	clientOpts := client.Options{}
	if spageAppConfig.Temporal.Address != "" {
		clientOpts.HostPort = spageAppConfig.Temporal.Address
		log.Printf("Temporal client configured with address: %s", spageAppConfig.Temporal.Address)
	} else {
		log.Println("Temporal client using default address (localhost:7233 or TEMPORAL_GRPC_ENDPOINT).")
	}

	temporalClient, err := client.Dial(clientOpts)
	if err != nil {
		log.Fatalf("Unable to create Temporal client: %v", err)
	}
	defer temporalClient.Close()
	log.Println("Temporal client connected.")

	var spageAppInventory *pkg.Inventory
	if opts.InventoryPath != "" {
		spageAppInventory, err = pkg.LoadInventory(opts.InventoryPath)
		if err != nil {
			log.Fatalf("Failed to load Spage inventory file '%s': %v", opts.InventoryPath, err)
		}
		log.Printf("Spage inventory loaded from '%s'.", opts.InventoryPath)
	} else {
		log.Println("No Spage inventory file specified. Creating a default localhost inventory.")
		spageAppInventory = &pkg.Inventory{
			Hosts: map[string]*pkg.Host{
				"localhost": {
					Name:    "localhost",
					IsLocal: true,
					Host:    "localhost",
					Vars:    make(map[string]interface{}),
				},
			},
		}
	}
	if err := opts.Graph.CheckInventoryForRequiredInputs(spageAppInventory); err != nil {
		log.Fatalf("Inventory check failed for required inputs: %v", err)
	}

	taskQueue := spageAppConfig.Temporal.TaskQueue
	if taskQueue == "" {
		taskQueue = "SPAGE_DEFAULT_TASK_QUEUE"
		log.Printf("TaskQueue from config is empty, using emergency default: %s", taskQueue)
	} else {
		log.Printf("Using Temporal TaskQueue from config: %s", taskQueue)
	}
	myWorker := worker.New(temporalClient, taskQueue, worker.Options{})

	myWorker.RegisterWorkflow(SpageTemporalWorkflow)
	myWorker.RegisterActivity(ExecuteSpageTaskActivity)

	log.Printf("Starting Temporal worker on task queue '%s'...", taskQueue)
	if err := myWorker.Start(); err != nil {
		log.Fatalf("Unable to start worker: %v", err)
	}
	log.Println("Worker started successfully.")

	if spageAppConfig.Temporal.Trigger {
		log.Println("Attempting to start the SpageTemporalWorkflow based on configuration...")
		workflowIDPrefix := spageAppConfig.Temporal.WorkflowIDPrefix
		if workflowIDPrefix == "" {
			workflowIDPrefix = "spage-workflow"
			log.Printf("WorkflowIDPrefix from config is empty, using emergency default: %s", workflowIDPrefix)
		}
		workflowID := workflowIDPrefix + "-" + uuid.New().String()

		workflowOptions := client.StartWorkflowOptions{
			ID:        workflowID,
			TaskQueue: taskQueue,
		}

		common.LogDebug("Executing workflow with graph, inventory, and config.", map[string]interface{}{
			"graph_tasks_count":     len(opts.Graph.Tasks),
			"inventory_hosts_count": len(spageAppInventory.Hosts),
			"config_mode":           spageAppConfig.ExecutionMode,
		})

		we, err := temporalClient.ExecuteWorkflow(context.Background(), workflowOptions, SpageTemporalWorkflow, opts.Graph, spageAppInventory, spageAppConfig)
		if err != nil {
			log.Fatalf("Unable to execute SpageTemporalWorkflow: %v", err)
		}
		log.Println("Successfully started SpageTemporalWorkflow", "WorkflowID", we.GetID(), "RunID", we.GetRunID())

		log.Println("Waiting for workflow to complete...", "WorkflowID", we.GetID())
		err = we.Get(context.Background(), nil)
		if err != nil {
			log.Fatalf("Workflow %s completed with error: %v", we.GetID(), err)
		} else {
			log.Println("Workflow completed successfully.", "WorkflowID", we.GetID())
			myWorker.Stop()
		}
	} else {
		log.Println("Application setup complete. Worker is running. Press Ctrl+C to exit.")
		<-worker.InterruptCh()
		log.Println("Shutting down worker...")
		myWorker.Stop()
		log.Println("Worker stopped.")
	}
}
