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

	// Construct a unique and descriptive ActivityID
	activityID := fmt.Sprintf("%s-%s-%s", task.Name, closure.HostContext.Host.Name, uuid.New().String())

	ao := workflow.ActivityOptions{
		ActivityID:          activityID,
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

	// Construct a unique and descriptive ActivityID for the revert task
	activityID := fmt.Sprintf("revert-%s-%s-%s", task.Name, closure.HostContext.Host.Name, uuid.New().String())

	ao := workflow.ActivityOptions{
		ActivityID:          activityID,
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
	future := workflow.ExecuteActivity(temporalAwareCtx, RevertSpageTaskActivity, activityInput)
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

// RevertSpageTaskActivity is the generic activity that runs a Spage task's revert action.
func RevertSpageTaskActivity(ctx context.Context, input SpageActivityInput) (*SpageActivityResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("RevertSpageTaskActivity started", "task", input.TaskDefinition.Name, "host", input.TargetHost.Name)
	activity.RecordHeartbeat(ctx, fmt.Sprintf("Starting revert for task %s on host %s", input.TaskDefinition.Name, input.TargetHost.Name))

	hostCtx, err := pkg.InitializeHostContext(&input.TargetHost)
	if err != nil {
		logger.Error("Failed to initialize host context for revert", "host", input.TargetHost.Name, "task", input.TaskDefinition.Name, "error", err)
		return &SpageActivityResult{
			HostName: input.TargetHost.Name,
			TaskName: "revert-" + input.TaskDefinition.Name,
			Error:    fmt.Sprintf("failed to initialize host context for revert task %s: %v", input.TaskDefinition.Name, err),
		}, nil
	}
	defer hostCtx.Close()

	if input.CurrentHostFacts != nil {
		for k, v := range input.CurrentHostFacts {
			hostCtx.Facts.Store(k, v)
		}
	}

	closure := pkg.ConstructClosure(hostCtx, input.TaskDefinition)

	if input.LoopItem != nil {
		loopVarName := "item"
		closure.ExtraFacts[loopVarName] = input.LoopItem
		logger.Debug("Loop item added to closure facts for revert", "loopVar", loopVarName, "value", input.LoopItem)
	}

	taskResult := input.TaskDefinition.RevertModule(closure) // Changed to RevertModule
	activity.RecordHeartbeat(ctx, fmt.Sprintf("Finished revert for task %s on host %s", input.TaskDefinition.Name, input.TargetHost.Name))

	result := &SpageActivityResult{
		HostName:       input.TargetHost.Name,
		TaskName:       "revert-" + input.TaskDefinition.Name, // Prefix task name for clarity
		RegisteredVars: make(map[string]interface{}),
	}

	var ignoredError *pkg.IgnoredTaskError
	if errors.As(taskResult.Error, &ignoredError) {
		result.Ignored = true
		originalErr := ignoredError.Unwrap()
		result.Error = originalErr.Error()
		logger.Warn("Revert task failed but error was ignored", "task", input.TaskDefinition.Name, "originalError", originalErr)
		failureMap := map[string]interface{}{
			"failed":  true,
			"changed": false,
			"msg":     originalErr.Error(),
			"ignored": true,
		}
		if taskResult.Output != nil {
			if factProvider, ok := taskResult.Output.(pkg.FactProvider); ok {
				outputFacts := factProvider.AsFacts()
				for k, v := range outputFacts {
					failureMap[k] = v
				}
			}
		}
		if input.TaskDefinition.Register != "" { // Note: Register on revert might be unusual but technically possible
			result.RegisteredVars[input.TaskDefinition.Register] = failureMap
		}
	} else if taskResult.Error != nil {
		result.Error = taskResult.Error.Error()
		logger.Error("Revert task execution failed", "task", input.TaskDefinition.Name, "error", taskResult.Error)
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
			logger.Info("Revert task executed successfully", "task", input.TaskDefinition.Name, "changed", result.Changed)
			if input.TaskDefinition.Register != "" {
				valueToStore := pkg.ConvertOutputToFactsMap(taskResult.Output)
				result.RegisteredVars[input.TaskDefinition.Register] = valueToStore
				logger.Info("Variable registered during revert", "task", input.TaskDefinition.Name, "variable", input.TaskDefinition.Register)
			}
		} else {
			result.Skipped = true // A revert might be skipped if not applicable
			logger.Info("Revert task executed, no output (potentially skipped)", "task", input.TaskDefinition.Name)
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

	result.HostFactsSnapshot = make(map[string]interface{})
	hostCtx.Facts.Range(func(key, value interface{}) bool {
		if kStr, ok := key.(string); ok {
			result.HostFactsSnapshot[kStr] = value
		} else {
			logger.Warn("Non-string key found in HostContext facts during revert snapshot", "key_type", fmt.Sprintf("%T", key), "key_value", key)
		}
		return true
	})

	return result, nil
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
	hostContexts map[string]*pkg.HostContext,
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
		countForBuffer, err := CalculateExpectedResults(tasksInLevel, hostContexts)
		if err != nil {
			errMsg := fmt.Errorf("critical error during pre-count for task closures: %w", err)
			common.LogError("Dispatch error in loadLevelTasks (pre-count)", map[string]interface{}{"error": errMsg})
			errCh.SendAsync(errMsg)
			return // Abort if counting fails
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
		task := taskDefinition

		// Handle run_once tasks separately
		if task.RunOnce {
			// Get the first available host
			firstHostCtx, firstHostName := GetFirstAvailableHost(hostContexts)

			if firstHostCtx == nil {
				errMsg := fmt.Errorf("no hosts available for run_once task '%s'", task.Name)
				common.LogError("No hosts available for run_once task", map[string]interface{}{"error": errMsg})
				errCh.SendAsync(errMsg)
				return
			}

			// Execute only on the first host
			closures, err := GetTaskClosures(task, firstHostCtx)
			if err != nil {
				errMsg := fmt.Errorf("critical error: failed to get task closures for run_once task '%s' on host '%s': %w", task.Name, firstHostName, err)
				common.LogError("Dispatch error for run_once task", map[string]interface{}{"error": errMsg})
				errCh.SendAsync(errMsg)
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
						errCh.SendAsync(errMsg)
						return
					}
					if delegatedHostContext != nil {
						// Update the closure to use the delegated host context
						closure.HostContext = delegatedHostContext
					}
				}

				logger.Info("Executing run_once task", "task", task.Name, "execution_host", firstHostName, "total_hosts", len(hostContexts))

				if isParallelDispatch {
					actualDispatchedTasks++ // We'll dispatch one execution but generate results for all hosts
					workflow.Go(workflowCtx, func(childTaskCtx workflow.Context) {
						logger.Debug("Run_once task coroutine started", "task", task.Name, "host", firstHostName)
						originalResult := e.Runner.ExecuteTask(childTaskCtx, task, closure, cfg)

						// Create results for all hosts based on the original execution
						allResults := CreateRunOnceResultsForAllHosts(originalResult, hostContexts, firstHostName)

						// Send results for all hosts
						for _, result := range allResults {
							logger.Debug("Sending run_once result to resultsCh", "task", task.Name, "target_host", result.Closure.HostContext.Host.Name)
							resultsCh.SendAsync(result)
						}

						logger.Debug("Attempting to send to completionCh for run_once", "task", task.Name, "host", firstHostName)
						completionCh.SendAsync(true)
						logger.Debug("Successfully sent to completionCh for run_once", "task", task.Name, "host", firstHostName)
					})
				} else {
					logger.Debug("Executing sequential run_once task", "task", task.Name, "host", firstHostName)
					originalResult := e.Runner.ExecuteTask(workflowCtx, task, closure, cfg)

					// Create results for all hosts based on the original execution
					allResults := CreateRunOnceResultsForAllHosts(originalResult, hostContexts, firstHostName)

					// Send results for all hosts
					for _, result := range allResults {
						logger.Debug("Sending sequential run_once result to resultsCh", "task", task.Name, "target_host", result.Closure.HostContext.Host.Name)
						resultsCh.SendAsync(result)
					}
				}
			}

			// Continue to next task since run_once is handled
			continue
		}

		// Normal task execution for non-run_once tasks
		for hostName, hostCtx := range hostContexts {
			closures, err := GetTaskClosures(task, hostCtx)
			if err != nil {
				errMsg := fmt.Errorf("critical error: failed to get task closures for task '%s' on host '%s': %w. Aborting level", task.Name, hostName, err)
				common.LogError("Dispatch error in loadLevelTasks", map[string]interface{}{"error": errMsg})
				errCh.SendAsync(errMsg)
				return
			}

			for _, individualClosure := range closures {
				closure := individualClosure

				if task.DelegateTo != "" {
					delegatedHostContext, err := GetDelegatedHostContext(task, hostContexts, closure)
					if err != nil {
						errMsg := fmt.Errorf("failed to resolve delegate_to for task '%s': %w", task.Name, err)
						common.LogError("Delegate resolution error in loadLevelTasks", map[string]interface{}{"error": errMsg})
						errCh.SendAsync(errMsg)
						return
					}
					if delegatedHostContext != nil {
						closure.HostContext = delegatedHostContext
					}
				}

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
) (bool, []pkg.TaskResult, error) {
	ctx := e.Runner.WorkflowCtx
	logger := workflow.GetLogger(ctx)

	var levelHardErrored bool
	resultsReceived := 0
	var dispatchError error
	resultsChActive := true
	errChActive := true
	var processedTasksOnLevel []pkg.TaskResult // To store task results for this level

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
		processedTasksOnLevel = append(processedTasksOnLevel, result) // Store the result

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
		return true, processedTasksOnLevel, dispatchError
	}

	if !resultsChActive && resultsReceived < numExpectedResultsOnLevel {
		errMsg := fmt.Errorf("level %d results channel closed prematurely: got %d, expected %d. loadLevelTasks might have an issue", executionLevel, resultsReceived, numExpectedResultsOnLevel)
		logger.Error(errMsg.Error())
		return true, processedTasksOnLevel, errMsg
	}

	if resultsReceived < numExpectedResultsOnLevel {
		errMsg := fmt.Errorf("level %d did not receive all expected results: got %d, expected %d. resultsCh may have closed prematurely or loadLevelTasks did not send all results", executionLevel, resultsReceived, numExpectedResultsOnLevel)
		logger.Error(errMsg.Error())
		return true, processedTasksOnLevel, errMsg
	}

	logger.Info("All expected results received for level.", "level", executionLevel, "count", resultsReceived)
	return levelHardErrored, processedTasksOnLevel, nil
}

func (e *TemporalGraphExecutor) Execute(
	hostContexts map[string]*pkg.HostContext,
	orderedGraph [][]pkg.Task,
	cfg *config.Config,
) error {
	// For Temporal, we need to manage facts in a workflow-safe map.
	// We extract initial facts, and then this map becomes the source of truth.
	// The HostContext objects passed to utility functions will be updated from this map.
	workflowHostFacts := make(map[string]map[string]interface{})
	for hostName, hostCtx := range hostContexts {
		hostFacts := make(map[string]interface{})
		hostCtx.Facts.Range(func(key, value interface{}) bool {
			if kStr, ok := key.(string); ok {
				hostFacts[kStr] = value
			}
			return true
		})
		workflowHostFacts[hostName] = hostFacts
	}

	// recapStats and executionHistory would be initialized and managed here if needed for Temporal version
	// For now, focusing on fact propagation. RecapStats are not used by processLevelResults anymore.
	// ExecutionHistory for revert needs careful design for Temporal.
	workflowCtx := e.Runner.WorkflowCtx                    // Get the root workflow context for Revert if needed
	logger := workflow.GetLogger(workflowCtx)              // Define logger for Execute scope
	executionTaskResults := make(map[int][]pkg.TaskResult) // Store all successful/processed task results per level

	for executionLevel, tasksInLevel := range orderedGraph {
		// Before each level, ensure the hostContexts have the latest facts from the workflow state.
		// This is crucial for when/loop/delegate_to conditions in utility functions.
		for _, hostCtx := range hostContexts {
			// Clear existing facts (they are stale from the previous level or initial state)
			hostCtx.Facts = &sync.Map{}
			if facts, ok := workflowHostFacts[hostCtx.Host.Name]; ok {
				for k, v := range facts {
					hostCtx.Facts.Store(k, v)
				}
			}
		}

		numExpectedResultsOnLevel, err := CalculateExpectedResults(tasksInLevel, hostContexts)
		if err != nil {
			return fmt.Errorf("failed to calculate expected results for level %d: %w", executionLevel, err)
		}

		if numExpectedResultsOnLevel == 0 && len(tasksInLevel) > 0 {
			workflow.GetLogger(e.Runner.WorkflowCtx).Info("No task instances to execute for level, skipping.", "level", executionLevel)
			continue
		}

		resultsCh := workflow.NewBufferedChannel(e.Runner.WorkflowCtx, numExpectedResultsOnLevel)
		errCh := workflow.NewChannel(e.Runner.WorkflowCtx)

		workflow.Go(e.Runner.WorkflowCtx, func(ctx workflow.Context) {
			e.loadLevelTasks(ctx, tasksInLevel, hostContexts, resultsCh, errCh, cfg)
		})

		levelErrored, processedResultsThisLevel, errProcessingResults := e.processLevelResults(
			resultsCh, errCh,
			workflowHostFacts,
			executionLevel,
			cfg, numExpectedResultsOnLevel,
		)
		if errProcessingResults != nil {
			return errProcessingResults
		}

		if len(processedResultsThisLevel) > 0 {
			executionTaskResults[executionLevel] = processedResultsThisLevel
		}

		if errProcessingResults != nil {
			// If processLevelResults itself returns an error (e.g., premature channel close), attempt revert
			logger.Error("Error processing results, attempting revert", "level", executionLevel, "error", errProcessingResults)
			if revertErr := e.revertWorkflow(workflowCtx, executionTaskResults, hostContexts, workflowHostFacts, cfg, executionLevel); revertErr != nil {
				return fmt.Errorf("error during graph execution on level %d (%v) and also during revert: %w", executionLevel, errProcessingResults, revertErr)
			}
			return fmt.Errorf("error during graph execution on level %d: %w, tasks reverted", executionLevel, errProcessingResults)
		}

		if levelErrored {
			logger.Info("Run failed, task reversion required", map[string]interface{}{"level": executionLevel})
			if revertErr := e.revertWorkflow(workflowCtx, executionTaskResults, hostContexts, workflowHostFacts, cfg, executionLevel); revertErr != nil {
				return fmt.Errorf("run failed on level %d and also failed during revert: %w", executionLevel, revertErr)
			}
			return fmt.Errorf("run failed on level %d and tasks reverted", executionLevel)
		}
	}
	return nil
}

// Revert implements the GraphExecutor interface for Temporal.
// It requires a workflow context, which must be passed in via the context.Context argument.
func (e *TemporalGraphExecutor) Revert(
	ctx context.Context,
	executedTasksHistory []map[string]chan pkg.Task,
	hostContexts map[string]*pkg.HostContext,
	cfg *config.Config,
) error {
	// The Temporal executor's Revert logic is deeply tied to the workflow's state and execution history.
	// The `executedTasksHistory` from the generic interface is not suitable for reconstructing the
	// necessary state (like loop items, specific closures, etc.) for a Temporal revert.
	// Revert operations are instead handled inside the `Execute` method, which has access
	// to the `workflow.Context` and the full `TaskResult` history.
	// This method is here to satisfy the interface, but the actual logic is in `revertWorkflow`.
	return fmt.Errorf("direct call to Revert on TemporalGraphExecutor is not supported. Reversion is handled within the workflow execution")
}

// revertWorkflow orchestrates the revert process for tasks up to a certain level within a workflow.
func (e *TemporalGraphExecutor) revertWorkflow(
	workflowCtx workflow.Context, // Main workflow context
	executedTasks map[int][]pkg.TaskResult, // History of tasks processed per level
	hostContexts map[string]*pkg.HostContext,
	workflowHostFacts map[string]map[string]interface{},
	cfg *config.Config,
	failingLevel int, // The level at or before which failure occurred
) error {
	logger := workflow.GetLogger(workflowCtx)
	logger.Info("Starting revert process", "up_to_level", failingLevel)
	var overallRevertError error

	// Revert from the failingLevel (or last successfully processed level part of it) down to 0
	for level := failingLevel; level >= 0; level-- {
		tasksToRevertOnLevel, levelExists := executedTasks[level]
		if !levelExists || len(tasksToRevertOnLevel) == 0 {
			logger.Info("No tasks to revert on level", "level", level)
			continue
		}

		logger.Info("Reverting tasks for level", "level", level, "num_tasks_to_revert", len(tasksToRevertOnLevel))

		revertResultsCh := workflow.NewBufferedChannel(workflowCtx, len(tasksToRevertOnLevel))
		// revertErrCh is for catastrophic dispatch errors, not individual task revert failures.
		// Individual task revert failures will be collected and will make overallRevertError non-nil.
		var revertCompletionCh workflow.Channel
		if cfg.ExecutionMode == "parallel" {
			revertCompletionCh = workflow.NewBufferedChannel(workflowCtx, len(tasksToRevertOnLevel))
		}

		actualRevertsDispatched := 0

		for _, taskResultToRevert := range tasksToRevertOnLevel {
			originalTask := taskResultToRevert.Task       // Capture for goroutine
			originalClosure := taskResultToRevert.Closure // Capture for goroutine

			if originalClosure == nil || originalClosure.HostContext == nil || originalClosure.HostContext.Host == nil {
				logger.Error("Cannot revert task due to nil Closure/HostContext/Host in stored TaskResult", "task_name", originalTask.Name)
				syntheticRevertResult := pkg.TaskResult{
					Task:    originalTask,
					Closure: originalClosure,
					Error:   fmt.Errorf("revert skipped for task %s due to missing context in original result", originalTask.Name),
					Status:  pkg.TaskStatusFailed,
				}
				revertResultsCh.SendAsync(syntheticRevertResult)
				if cfg.ExecutionMode == "parallel" && revertCompletionCh != nil {
					revertCompletionCh.SendAsync(true) // Still need to signal completion for the slot
				}
				continue
			}
			hostName := originalClosure.HostContext.Host.Name // Capture for goroutine

			// Create a new closure with up-to-date facts for the revert operation.
			// The original closure might have stale facts.
			clonedRevertClosure := &pkg.Closure{
				HostContext: &pkg.HostContext{
					Host:  originalClosure.HostContext.Host, // Use original host definition
					Facts: &sync.Map{},                      // Fresh facts map for this revert operation
					// SSHClient: nil, // Should not be needed or used in workflow activities
				},
				ExtraFacts: make(map[string]interface{}),
			}
			// Copy ExtraFacts (like loop item) from original closure
			for k, v := range originalClosure.ExtraFacts {
				clonedRevertClosure.ExtraFacts[k] = v
			}
			// Populate facts from current workflowHostFacts
			if currentFactsForHost, ok := workflowHostFacts[hostName]; ok {
				for k, v := range currentFactsForHost {
					clonedRevertClosure.HostContext.Facts.Store(k, v)
				}
			} else {
				logger.Warn("No current facts found for host during revert, revert task will use minimal facts", "host", hostName, "task", originalTask.Name)
			}

			actualRevertsDispatched++
			if cfg.ExecutionMode == "parallel" {
				workflow.Go(workflowCtx, func(revertCtx workflow.Context) {
					logger.Info("Dispatching revert task (parallel)", "task", originalTask.Name, "host", hostName)
					revertResult := e.Runner.RevertTask(revertCtx, originalTask, clonedRevertClosure, cfg)
					revertResultsCh.SendAsync(revertResult)
					if revertCompletionCh != nil {
						revertCompletionCh.SendAsync(true)
					}
				})
			} else {
				logger.Info("Dispatching revert task (sequential)", "task", originalTask.Name, "host", hostName)
				revertResult := e.Runner.RevertTask(workflowCtx, originalTask, clonedRevertClosure, cfg)
				revertResultsCh.SendAsync(revertResult)
			}
		}

		if cfg.ExecutionMode == "parallel" && actualRevertsDispatched > 0 {
			// Ensure revertCompletionCh is not nil before using it
			if revertCompletionCh != nil {
				for i := 0; i < actualRevertsDispatched; i++ {
					revertCompletionCh.Receive(workflowCtx, nil)
				}
				logger.Info("All parallel revert tasks completed signal-wise for level", "level", level)
			}
		}
		revertResultsCh.Close()

		var levelRevertHardErrored bool
		revertsReceivedThisLevel := 0
		for revertsReceivedThisLevel < actualRevertsDispatched {
			var revertResult pkg.TaskResult
			more := revertResultsCh.Receive(workflowCtx, &revertResult)
			if !more {
				if revertsReceivedThisLevel < actualRevertsDispatched {
					logger.Error("Revert results channel closed prematurely", "level", level, "received", revertsReceivedThisLevel, "expected", actualRevertsDispatched)
					levelRevertHardErrored = true
					break
				}
				break
			}
			revertsReceivedThisLevel++

			if revertResult.Closure != nil && revertResult.Closure.HostContext != nil && revertResult.Closure.HostContext.Host != nil {
				hostName := revertResult.Closure.HostContext.Host.Name
				taskName := revertResult.Task.Name
				if activityOutput, ok := revertResult.ExecutionSpecificOutput.(SpageActivityResult); ok {
					if errFact := processActivityResultAndRegisterFacts(workflowCtx, activityOutput, "revert-"+taskName, hostName, workflowHostFacts); errFact != nil {
						logger.Error("Reverted task reported an error after fact registration", "task", taskName, "host", hostName, "original_error", errFact)
						levelRevertHardErrored = true
					} else if activityOutput.Error != "" && !activityOutput.Ignored { // Check activityOutput.Error even if errFact is nil, if not ignored
						logger.Error("Revert task failed (reported by activity)", "task", taskName, "host", hostName, "activity_error", activityOutput.Error)
						levelRevertHardErrored = true
					} else {
						logger.Info("Revert task processed", "task", taskName, "host", hostName, "changed", activityOutput.Changed, "skipped", activityOutput.Skipped, "ignored", activityOutput.Ignored)
					}
				} else if revertResult.Error != nil { // If no SpageActivityResult, check TaskResult.Error
					logger.Error("Revert task failed (TaskResult.Error)", "task", taskName, "host", hostName, "error", revertResult.Error)
					levelRevertHardErrored = true
				} else {
					logger.Info("Revert task completed (no specific activity result)", "task", taskName, "host", hostName, "changed", revertResult.Changed)
				}
			} else if revertResult.Error != nil { // Closure or context was nil, but there's an error
				logger.Error("Revert task failed (context missing in original result or other error)", "task_name", revertResult.Task.Name, "error", revertResult.Error)
				levelRevertHardErrored = true
			}
		}

		if levelRevertHardErrored {
			logger.Error("Hard error occurred during revert for level, marking overall revert as failed.", "level", level)
			overallRevertError = fmt.Errorf("revert failed on level %d", level) // Mark that at least one level's revert had issues
		}
	}

	logger.Info("Revert process finished.")
	return overallRevertError
}

// SpageTemporalWorkflow defines the main workflow logic.
func SpageTemporalWorkflow(ctx workflow.Context, graphInput *pkg.Graph, inventoryFile string, spageConfigInput *config.Config) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("SpageTemporalWorkflow started", "workflowId", workflow.GetInfo(ctx).WorkflowExecution.ID)

	temporalRunner := NewTemporalTaskRunner(ctx)
	e := &TemporalGraphExecutor{Runner: *temporalRunner}

	err := pkg.ExecuteGraph(e, *graphInput, inventoryFile, spageConfigInput)

	if err != nil {
		logger.Error("SpageTemporalWorkflow failed", "error", err)
		return err
	}

	logger.Info("SpageTemporalWorkflow completed successfully.")
	return nil
}

// RunSpageTemporalWorkerAndWorkflowOptions defines options for RunSpageTemporalWorkerAndWorkflow.
type RunSpageTemporalWorkerAndWorkflowOptions struct {
	Graph            *pkg.Graph
	InventoryPath    string
	LoadedConfig     *config.Config // Changed from ConfigPath to break import cycle with cmd
	WorkflowIDPrefix string
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
	myWorker.RegisterActivity(RevertSpageTaskActivity) // Register the new activity

	log.Printf("Starting Temporal worker on task queue '%s'...", taskQueue)
	if err := myWorker.Start(); err != nil {
		log.Fatalf("Unable to start worker: %v", err)
	}
	log.Println("Worker started successfully.")

	if spageAppConfig.Temporal.Trigger {
		log.Println("Attempting to start the SpageTemporalWorkflow based on configuration...")
		workflowIDPrefix := opts.WorkflowIDPrefix
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
			"graph_tasks_count": len(opts.Graph.Tasks),
			"inventory_path":    opts.InventoryPath,
			"config_mode":       spageAppConfig.ExecutionMode,
		})

		we, err := temporalClient.ExecuteWorkflow(context.Background(), workflowOptions, SpageTemporalWorkflow, opts.Graph, opts.InventoryPath, spageAppConfig)
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
