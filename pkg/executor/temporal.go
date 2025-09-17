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

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
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
	TaskHistory      map[string]interface{}
	Handlers         []GraphNodeDTO // Handlers from the graph (DTO for serialization)
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
	ModuleOutputMap   map[string]interface{}
	NotifiedHandlers  []string // Handler names that were notified during this activity
}

// GraphNodeDTO provides a serializable wrapper for pkg.GraphNode (Task or TaskCollection)
type GraphNodeDTO struct {
	Kind       string             `json:"kind"` // "task" | "collection"
	Task       *pkg.Task          `json:"task,omitempty"`
	Collection *TaskCollectionDTO `json:"collection,omitempty"`
}

// TaskCollectionDTO serializes TaskCollection
type TaskCollectionDTO struct {
	Id     int            `json:"id"`
	Name   string         `json:"name"`
	Tasks  []GraphNodeDTO `json:"tasks"`
	Rescue []GraphNodeDTO `json:"rescue,omitempty"`
	Always []GraphNodeDTO `json:"always,omitempty"`
}

func toGraphNodeDTO(n pkg.GraphNode) GraphNodeDTO {
	if t, ok := n.(*pkg.Task); ok {
		taskCopy := *t
		return GraphNodeDTO{Kind: "task", Task: &taskCopy}
	}
	if c, ok := n.(*pkg.MetaTask); ok {
		var children []GraphNodeDTO
		for _, child := range c.Children {
			children = append(children, toGraphNodeDTO(child))
		}
		var rescue []GraphNodeDTO
		for _, r := range c.Rescue {
			rescue = append(rescue, toGraphNodeDTO(r))
		}
		var always []GraphNodeDTO
		for _, a := range c.Always {
			always = append(always, toGraphNodeDTO(a))
		}
		return GraphNodeDTO{Kind: "collection", Collection: &TaskCollectionDTO{Id: c.Id, Name: c.Name, Tasks: children, Rescue: rescue, Always: always}}
	}
	return GraphNodeDTO{Kind: "unknown"}
}

func fromGraphNodeDTO(dto GraphNodeDTO) pkg.GraphNode {
	switch dto.Kind {
	case "task":
		if dto.Task == nil {
			return nil
		}
		taskCopy := *dto.Task
		return &taskCopy
	case "collection":
		if dto.Collection == nil {
			return nil
		}
		tc := &pkg.MetaTask{TaskParams: &pkg.TaskParams{Id: dto.Collection.Id, Name: dto.Collection.Name}}
		for _, child := range dto.Collection.Tasks {
			if gn := fromGraphNodeDTO(child); gn != nil {
				tc.Children = append(tc.Children, gn)
			}
		}
		for _, child := range dto.Collection.Rescue {
			if gn := fromGraphNodeDTO(child); gn != nil {
				tc.Rescue = append(tc.Rescue, gn)
			}
		}
		for _, child := range dto.Collection.Always {
			if gn := fromGraphNodeDTO(child); gn != nil {
				tc.Always = append(tc.Always, gn)
			}
		}
		return tc
	default:
		return nil
	}
}

// FormattedGenericOutput preserves the formatted string output from modules
// while still implementing the ModuleOutput interface and providing map access
type FormattedGenericOutput struct {
	formattedString string
	moduleMap       map[string]interface{}
	changed         bool
}

// String returns the already formatted string from the original module
func (f FormattedGenericOutput) String() string {
	return f.formattedString
}

// Changed returns the changed status from the original module
func (f FormattedGenericOutput) Changed() bool {
	return f.changed
}

// Facts returns the module output map for fact registration
func (f FormattedGenericOutput) Facts() map[string]interface{} {
	return f.moduleMap
}

// NewFormattedGenericOutput creates a FormattedGenericOutput from activity result
func NewFormattedGenericOutput(output string, moduleMap map[string]interface{}, changed bool) FormattedGenericOutput {
	return FormattedGenericOutput{
		formattedString: output,
		moduleMap:       moduleMap,
		changed:         changed,
	}
}

// ExecuteSpageTaskActivity is the generic activity that runs a Spage task.
func ExecuteSpageTaskActivity(ctx context.Context, input SpageActivityInput) (*SpageActivityResult, error) {
	logger := activity.GetLogger(ctx)

	activity.RecordHeartbeat(ctx, fmt.Sprintf("Starting task %s on host %s", input.TaskDefinition.Name, input.TargetHost.Name))

	hostCtx, err := pkg.InitializeHostContext(&input.TargetHost, input.SpageCoreConfig)
	if err != nil {
		logger.Error("Failed to initialize host context", "host", input.TargetHost.Name, "task", input.TaskDefinition.Name, "error", err)
		return &SpageActivityResult{
			HostName: input.TargetHost.Name,
			TaskName: input.TaskDefinition.Name,
			Error:    fmt.Sprintf("failed to initialize host context for task %s: %v", input.TaskDefinition.Name, err),
		}, nil
	}
	defer func() {
		if closeErr := hostCtx.Close(); closeErr != nil {
			logger.Warn("Failed to close host context", "host", input.TargetHost.Name, "error", closeErr)
		}
	}()

	// Load facts from workflow (these are now pre-processed by GetInitialFactsForHost)
	if input.CurrentHostFacts != nil {
		for k, v := range input.CurrentHostFacts {
			hostCtx.Facts.Store(k, v)
		}
	}
	// The direct merge of input.TargetHost.Vars is removed as GetInitialFactsForHost now handles this layering.

	// Initialize handler tracker with handlers from the graph (convert DTOs)
	if len(input.Handlers) > 0 {
		nodes := make([]pkg.GraphNode, 0, len(input.Handlers))
		for _, dto := range input.Handlers {
			if gn := fromGraphNodeDTO(dto); gn != nil {
				nodes = append(nodes, gn)
			}
		}
		hostCtx.InitializeHandlerTracker(nodes)
	} else {
		hostCtx.InitializeHandlerTracker(nil)
	}

	closure := input.TaskDefinition.ConstructClosure(hostCtx, input.SpageCoreConfig)

	if input.LoopItem != nil {
		loopVarName := "item"
		// if input.TaskDefinition.LoopControl.LoopVar != "" { // Temporarily commented out
		// 	loopVarName = input.TaskDefinition.LoopControl.LoopVar
		// }
		closure.ExtraFacts[loopVarName] = input.LoopItem
	}

	// TODO: handle meta tasks
	taskResultCh := input.TaskDefinition.ExecuteModule(closure)
	taskResult := <-taskResultCh
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
			if converted, ok := pkg.ConvertOutputToFactsMap(taskResult.Output).(map[string]interface{}); ok {
				result.ModuleOutputMap = converted
			}
		} else {
			result.Skipped = true
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
	// However, we should NOT capture task-level variables that were added to HostContext.Facts during execution.
	// Task-level variables should only exist in closure.ExtraFacts and should not persist across tasks.
	result.HostFactsSnapshot = make(map[string]interface{})

	// Create a map of task-level variable names to exclude from persistence
	taskLevelVars := make(map[string]bool)
	if input.TaskDefinition.Vars != nil {
		if varsMap, ok := input.TaskDefinition.Vars.(map[string]interface{}); ok {
			for varName := range varsMap {
				taskLevelVars[varName] = true
			}
		}
	}

	hostCtx.Facts.Range(func(key, value interface{}) bool {
		if kStr, ok := key.(string); ok {
			// Only capture facts that are NOT task-level variables
			if !taskLevelVars[kStr] {
				result.HostFactsSnapshot[kStr] = value
			}
			// Non-string keys are ignored as they shouldn't exist in our fact system
		}
		return true
	})

	// Capture handler notifications from the activity's host context
	if hostCtx.HandlerTracker != nil {
		notifiedHandlerTasks := hostCtx.HandlerTracker.GetNotifiedHandlers()
		result.NotifiedHandlers = make([]string, len(notifiedHandlerTasks))
		for i, handler := range notifiedHandlerTasks {
			result.NotifiedHandlers[i] = handler.Params().Name
		}
		logger.Debug("Activity captured handler notifications", "host", input.TargetHost.Name, "task", input.TaskDefinition.Name, "notified_handlers", result.NotifiedHandlers)
	}

	return result, nil
}

// SpageRunOnceLoopActivityInput defines the input for run_once tasks with loops.
type SpageRunOnceLoopActivityInput struct {
	TaskDefinition   pkg.Task
	TargetHost       pkg.Host
	LoopItems        []interface{} // All loop items to execute
	CurrentHostFacts map[string]interface{}
	SpageCoreConfig  *config.Config
	Handlers         []GraphNodeDTO // Handlers from the graph (DTO)
}

// SpageRunOnceLoopActivityResult defines the output from run_once loop activity.
type SpageRunOnceLoopActivityResult struct {
	HostName          string
	TaskName          string
	LoopResults       []SpageActivityResult // Results for each loop iteration
	HostFactsSnapshot map[string]interface{}
}

// TemporalResultChannel implements ResultChannel for Temporal workflow channels
type TemporalResultChannel struct {
	ctx workflow.Context
	ch  workflow.ReceiveChannel
}

func NewTemporalResultChannel(ctx workflow.Context, ch workflow.ReceiveChannel) *TemporalResultChannel {
	return &TemporalResultChannel{ctx: ctx, ch: ch}
}

func (c *TemporalResultChannel) ReceiveResult() (pkg.TaskResult, bool, error) {
	var result pkg.TaskResult
	more := c.ch.Receive(c.ctx, &result)
	return result, more, nil
}

func (c *TemporalResultChannel) IsClosed() bool {
	// For Temporal channels, we handle this differently in the selector pattern
	return false
}

// TemporalErrorChannel implements ErrorChannel for Temporal workflow channels
type TemporalErrorChannel struct {
	ctx workflow.Context
	ch  workflow.ReceiveChannel
}

func NewTemporalErrorChannel(ctx workflow.Context, ch workflow.ReceiveChannel) *TemporalErrorChannel {
	return &TemporalErrorChannel{ctx: ctx, ch: ch}
}

func (c *TemporalErrorChannel) ReceiveError() (error, bool, error) {
	var err error
	more := c.ch.Receive(c.ctx, &err)
	return err, more, nil
}

func (c *TemporalErrorChannel) IsClosed() bool {
	return false
}

// TemporalLogger implements Logger for Temporal workflow logging
type TemporalLogger struct {
	logger log.Logger
}

func NewTemporalLogger(ctx workflow.Context) *TemporalLogger {
	return &TemporalLogger{logger: workflow.GetLogger(ctx)}
}

func (l *TemporalLogger) Error(msg string, args ...interface{}) {
	l.logger.Error(msg, args...)
}

func (l *TemporalLogger) Warn(msg string, args ...interface{}) {
	l.logger.Warn(msg, args...)
}

func (l *TemporalLogger) Info(msg string, args ...interface{}) {
	l.logger.Info(msg, args...)
}

func (l *TemporalLogger) Debug(msg string, args ...interface{}) {
	l.logger.Debug(msg, args...)
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
func (r *TemporalTaskRunner) ExecuteTask(execCtx workflow.Context, task pkg.GraphNode, closure *pkg.Closure, cfg *config.Config) pkg.TaskResult {
	// execCtx is the context of the coroutine calling ExecuteTask.
	// r.WorkflowCtx is the root workflow context, kept for logger or other global workflow info if needed, but execCtx for blocking ops.
	logger := workflow.GetLogger(execCtx) // Use execCtx for logger too for better context association

	currentHostFacts := closure.GetFacts() // Facts from the closure, prepared by SpageTemporalWorkflow
	loopItem := closure.ExtraFacts["item"]

	// Get handlers from the host context's handler tracker and convert to DTOs
	var handlers []GraphNodeDTO
	if closure.HostContext.HandlerTracker != nil {
		for _, h := range closure.HostContext.HandlerTracker.GetAllHandlers() {
			handlers = append(handlers, toGraphNodeDTO(h))
		}
	}

	// Dispatch based on node type
	switch n := task.(type) {
	case *pkg.Task:
		taskName := n.Params().Name
		activityInput := SpageActivityInput{
			TaskDefinition:   *n,
			TargetHost:       *closure.HostContext.Host,
			LoopItem:         loopItem,
			CurrentHostFacts: currentHostFacts,
			SpageCoreConfig:  cfg,
			Handlers:         handlers,
		}

		var activityOutput SpageActivityResult
		activityID := fmt.Sprintf("%s-%s-%s", taskName, closure.HostContext.Host.Name, uuid.New().String())
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
		temporalAwareCtx := workflow.WithActivityOptions(execCtx, ao)

		startTime := workflow.Now(execCtx)
		future := workflow.ExecuteActivity(temporalAwareCtx, ExecuteSpageTaskActivity, activityInput)
		errOnGet := future.Get(execCtx, &activityOutput)
		endTime := workflow.Now(execCtx)
		duration := endTime.Sub(startTime)

		if errOnGet != nil {
			logger.Error("Temporal activity future.Get() failed", "task", taskName, "host", closure.HostContext.Host.Name, "error", errOnGet)
			return pkg.TaskResult{Task: n, Closure: closure, Error: fmt.Errorf("activity %s on host %s failed to complete: %w", taskName, closure.HostContext.Host.Name, errOnGet), Status: pkg.TaskStatusFailed, Failed: true, Duration: duration}
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
			if !activityOutput.Ignored {
				finalStatus = pkg.TaskStatusFailed
			}
		} else if activityOutput.Changed {
			finalStatus = pkg.TaskStatusChanged
		}
		return pkg.TaskResult{Task: n, Closure: closure, Error: finalError, Status: finalStatus, Failed: (finalError != nil && !activityOutput.Ignored), Changed: activityOutput.Changed, Duration: duration, ExecutionSpecificOutput: activityOutput, Output: NewFormattedGenericOutput(activityOutput.Output, activityOutput.ModuleOutputMap, activityOutput.Changed)}

	case *pkg.MetaTask:
		input := SpageMetaWorkflowInput{
			Meta:             toGraphNodeDTO(n),
			TargetHost:       *closure.HostContext.Host,
			CurrentHostFacts: currentHostFacts,
			SpageCoreConfig:  cfg,
			Handlers:         handlers,
		}
		cwo := workflow.ChildWorkflowOptions{TaskQueue: workflow.GetInfo(execCtx).TaskQueueName}
		childCtx := workflow.WithChildOptions(execCtx, cwo)
		start := workflow.Now(execCtx)
		var metaResult SpageMetaWorkflowResult
		cf := workflow.ExecuteChildWorkflow(childCtx, SpageMetaWorkflow, input)
		err := cf.Get(childCtx, &metaResult)
		end := workflow.Now(execCtx)
		duration := end.Sub(start)
		var finalErr error
		if err != nil {
			finalErr = err
		} else if metaResult.Error != "" {
			finalErr = errors.New(metaResult.Error)
		}
		status := pkg.TaskStatusOk
		if finalErr != nil {
			status = pkg.TaskStatusFailed
		} else if metaResult.Changed {
			status = pkg.TaskStatusChanged
		}
		// Store meta workflow result as ExecutionSpecificOutput so temporal path can register facts/handlers
		return pkg.TaskResult{Task: n, Closure: closure, Error: finalErr, Status: status, Failed: finalErr != nil, Changed: metaResult.Changed, Duration: duration, ExecutionSpecificOutput: metaResult}

	default:
		return pkg.TaskResult{Task: task, Closure: closure, Error: fmt.Errorf("unsupported node type %T", task), Status: pkg.TaskStatusFailed, Failed: true}
	}
}

func (r *TemporalTaskRunner) RevertTask(execCtx workflow.Context, task pkg.Task, closure *pkg.Closure, cfg *config.Config) pkg.TaskResult {
	// execCtx is the context of the coroutine calling RevertTask.
	logger := workflow.GetLogger(execCtx) // Use execCtx for logger

	currentHostFacts := closure.GetFacts()
	loopItem := closure.ExtraFacts["item"]

	taskHistory := make(map[string]interface{})
	if closure.HostContext != nil && closure.HostContext.History != nil {
		closure.HostContext.History.Range(func(key, value interface{}) bool {
			if kStr, ok := key.(string); ok {
				taskHistory[kStr] = value
			}
			return true
		})
	}

	// Get handlers from the host context's handler tracker and convert to DTOs
	var handlers []GraphNodeDTO
	if closure.HostContext.HandlerTracker != nil {
		for _, h := range closure.HostContext.HandlerTracker.GetAllHandlers() {
			handlers = append(handlers, toGraphNodeDTO(h))
		}
	}

	activityInput := SpageActivityInput{
		TaskDefinition:   task,
		TargetHost:       *closure.HostContext.Host,
		LoopItem:         loopItem,
		CurrentHostFacts: currentHostFacts,
		SpageCoreConfig:  cfg,
		TaskHistory:      taskHistory,
		Handlers:         handlers,
	}

	var activityOutput SpageActivityResult // Use a distinct name from the input

	// Construct a unique and descriptive ActivityID for the revert task
	activityID := fmt.Sprintf("revert-%s-%s-%s", task.Name, closure.HostContext.Host.Name, uuid.New().String())

	ao := workflow.ActivityOptions{
		ActivityID:          activityID,
		StartToCloseTimeout: 5 * time.Minute,  // Reduced from 30 minutes for faster test execution
		HeartbeatTimeout:    30 * time.Second, // Reduced from 2 minutes
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    500 * time.Millisecond, // Faster initial retry
			BackoffCoefficient: 1.5,                    // Reduced backoff
			MaximumInterval:    10 * time.Second,       // Reduced maximum interval
			MaximumAttempts:    2,                      // Reduced attempts for faster failure
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
		Output:                  NewFormattedGenericOutput(activityOutput.Output, activityOutput.ModuleOutputMap, activityOutput.Changed),
	}
}

// RevertSpageTaskActivity is the generic activity that runs a Spage task's revert action.
func RevertSpageTaskActivity(ctx context.Context, input SpageActivityInput) (*SpageActivityResult, error) {
	logger := activity.GetLogger(ctx)

	logger.Debug("RevertSpageTaskActivity started", "task", input.TaskDefinition.Name, "host", input.TargetHost.Name)
	activity.RecordHeartbeat(ctx, fmt.Sprintf("Starting revert for task %s on host %s", input.TaskDefinition.Name, input.TargetHost.Name))

	hostCtx, err := pkg.InitializeHostContext(&input.TargetHost, input.SpageCoreConfig)
	if err != nil {
		logger.Error("Failed to initialize host context for revert", "host", input.TargetHost.Name, "task", input.TaskDefinition.Name, "error", err)
		return &SpageActivityResult{
			HostName: input.TargetHost.Name,
			TaskName: "revert-" + input.TaskDefinition.Name,
			Error:    fmt.Sprintf("failed to initialize host context for revert task %s: %v", input.TaskDefinition.Name, err),
		}, nil
	}
	defer func() {
		if closeErr := hostCtx.Close(); closeErr != nil {
			logger.Warn("Failed to close host context for revert", "host", input.TargetHost.Name, "error", closeErr)
		}
	}()

	if input.CurrentHostFacts != nil {
		for k, v := range input.CurrentHostFacts {
			hostCtx.Facts.Store(k, v)
		}
	}

	if input.TaskHistory != nil {
		for k, v := range input.TaskHistory {
			hostCtx.History.Store(k, v)
		}
	}

	// Initialize handler tracker with handlers from the graph (convert DTOs)
	if len(input.Handlers) > 0 {
		nodes := make([]pkg.GraphNode, 0, len(input.Handlers))
		for _, dto := range input.Handlers {
			if gn := fromGraphNodeDTO(dto); gn != nil {
				nodes = append(nodes, gn)
			}
		}
		hostCtx.InitializeHandlerTracker(nodes)
	} else {
		hostCtx.InitializeHandlerTracker(nil)
	}

	closure := input.TaskDefinition.ConstructClosure(hostCtx, input.SpageCoreConfig)

	if input.LoopItem != nil {
		loopVarName := "item"
		closure.ExtraFacts[loopVarName] = input.LoopItem
		logger.Debug("Loop item added to closure facts for revert", "loopVar", loopVarName, "value", input.LoopItem)
	}

	// TODO: handle meta tasks
	taskResultCh := input.TaskDefinition.RevertModule(closure) // Changed to RevertModule
	taskResult := <-taskResultCh
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
			result.ModuleOutputMap = pkg.ConvertOutputToFactsMap(taskResult.Output).(map[string]interface{})
			logger.Debug("Revert task executed successfully", "task", input.TaskDefinition.Name, "changed", result.Changed)
			if input.TaskDefinition.Register != "" {
				valueToStore := pkg.ConvertOutputToFactsMap(taskResult.Output)
				result.RegisteredVars[input.TaskDefinition.Register] = valueToStore
				logger.Debug("Variable registered during revert", "task", input.TaskDefinition.Name, "variable", input.TaskDefinition.Register)
			}
		} else {
			result.Skipped = true // A revert might be skipped if not applicable
			logger.Debug("Revert task executed, no output (potentially skipped)", "task", input.TaskDefinition.Name)
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
			logger.Debug("Non-string key found in HostContext facts during revert snapshot", "key_type", fmt.Sprintf("%T", key), "key_value", key)
		}
		return true
	})

	// Capture handler notifications from the revert activity's host context
	if hostCtx.HandlerTracker != nil {
		notifiedHandlerTasks := hostCtx.HandlerTracker.GetNotifiedHandlers()
		result.NotifiedHandlers = make([]string, len(notifiedHandlerTasks))
		for i, handler := range notifiedHandlerTasks {
			result.NotifiedHandlers[i] = handler.Params().Name
		}
		logger.Debug("Revert activity captured handler notifications", "host", input.TargetHost.Name, "task", input.TaskDefinition.Name, "notified_handlers", result.NotifiedHandlers)
	}

	return result, nil
}

type TemporalGraphExecutor struct {
	Runner TemporalTaskRunner
}

func NewTemporalGraphExecutor(runner TemporalTaskRunner) *TemporalGraphExecutor {
	return &TemporalGraphExecutor{Runner: runner}
}

// executeRunOnceWithAllLoops executes a run_once task with all its loop iterations
// as a single deterministic activity, then replicates the results to all hosts
func (e *TemporalGraphExecutor) loadLevelTasks(
	workflowCtx workflow.Context,
	tasksInLevel []pkg.GraphNode,
	hostContexts map[string]*pkg.HostContext,
	resultsCh workflow.Channel,
	errCh workflow.Channel,
	cfg *config.Config,
	workflowHostFacts map[string]map[string]interface{},
) {
	defer resultsCh.Close()
	defer errCh.Close()

	isParallelDispatch := cfg.ExecutionMode == "parallel"
	var completionCh workflow.Channel

	if isParallelDispatch {
		countForBuffer, err := CalculateExpectedResults(tasksInLevel, hostContexts, cfg)
		if err != nil {
			errMsg := fmt.Errorf("critical error during pre-count for task closures: %w", err)
			common.LogError("Dispatch error in loadLevelTasks (pre-count)", map[string]interface{}{"error": errMsg})
			errCh.SendAsync(errMsg)
			return // Abort if counting fails
		}

		if countForBuffer > 0 {
			completionCh = workflow.NewBufferedChannel(workflowCtx, countForBuffer)
		}
	}

	adapter := NewTemporalRunnerAdapter(workflowCtx, &e.Runner, cfg)
	env := NewTemporalDispatchEnv(workflowCtx, resultsCh, errCh, completionCh, isParallelDispatch, adapter)
	SharedLoadLevelTasks(env, adapter, tasksInLevel, hostContexts, cfg, workflowHostFacts)
}

func (e *TemporalGraphExecutor) processLevelResults(
	resultsCh workflow.Channel,
	errCh workflow.Channel,
	hostFacts map[string]map[string]interface{},
	executionLevel int,
	cfg *config.Config,
	numExpectedResultsOnLevel int,
	recapStats map[string]map[string]int,
	hostContexts map[string]*pkg.HostContext,
) (bool, []pkg.TaskResult, error) {
	ctx := e.Runner.WorkflowCtx

	// Create adapters for the shared function
	resultsChAdapter := NewTemporalResultChannel(ctx, resultsCh)
	errChAdapter := NewTemporalErrorChannel(ctx, errCh)
	logger := NewTemporalLogger(ctx)

	// Create the fact processing callback for temporal executor
	onResult := func(result pkg.TaskResult) error {
		if result.Closure == nil || result.Closure.HostContext == nil || result.Closure.HostContext.Host == nil {
			return fmt.Errorf("received TaskResult with nil Closure/HostContext/Host for task %s on level %d", result.Task.Params().Name, executionLevel)
		}

		hostname := result.Closure.HostContext.Host.Name
		taskName := result.Task.Params().Name

		if activitySpecificOutput, ok := result.ExecutionSpecificOutput.(SpageActivityResult); ok {
			return processActivityResultAndRegisterFacts(ctx, activitySpecificOutput, taskName, hostname, hostFacts, hostContexts)
		}
		if metaOutput, ok := result.ExecutionSpecificOutput.(SpageMetaWorkflowResult); ok {
			// Apply meta snapshot to workflow facts
			if _, ok := hostFacts[hostname]; !ok {
				hostFacts[hostname] = make(map[string]interface{})
			}
			for k, v := range metaOutput.HostFactsSnapshot {
				hostFacts[hostname][k] = v
			}
			// Apply notified handlers
			if hostCtx, exists := hostContexts[hostname]; exists && hostCtx.HandlerTracker != nil {
				for _, name := range metaOutput.NotifiedHandlers {
					hostCtx.HandlerTracker.NotifyHandler(name)
				}
			}
			return nil
		}
		return nil // Unknown type; do nothing
	}

	levelHardErrored, processedTasksOnLevel, err := SharedProcessLevelResults(
		resultsChAdapter,
		errChAdapter,
		logger,
		executionLevel,
		cfg,
		numExpectedResultsOnLevel,
		recapStats,
		nil,      // No execution history for temporal executor
		onResult, // Fact processing callback
	)

	return levelHardErrored, processedTasksOnLevel, err
}

func (e *TemporalGraphExecutor) Execute(
	hostContexts map[string]*pkg.HostContext,
	orderedGraph [][]pkg.GraphNode,
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
	recapStats := make(map[string]map[string]int)          // Initialize recap stats

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

		numExpectedResultsOnLevel, err := CalculateExpectedResults(tasksInLevel, hostContexts, cfg)
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
			e.loadLevelTasks(ctx, tasksInLevel, hostContexts, resultsCh, errCh, cfg, workflowHostFacts)
		})

		levelErrored, processedResultsThisLevel, errProcessingResults := e.processLevelResults(
			resultsCh, errCh,
			workflowHostFacts,
			executionLevel,
			cfg, numExpectedResultsOnLevel,
			recapStats,
			hostContexts,
		)
		if errProcessingResults != nil {
			// Execute handlers before reverting, as handlers should run for any tasks that successfully notified them
			if err := e.executeHandlers(workflowCtx, hostContexts, workflowHostFacts, recapStats, cfg); err != nil {
				logger.Warn("Failed to execute handlers before revert", "error", err.Error())
			}

			if !cfg.Revert {
				return fmt.Errorf("error during graph execution on level %d: %w. Revert disabled", executionLevel, errProcessingResults)
			}
			// If processLevelResults itself returns an error (e.g., premature channel close), attempt revert
			logger.Error("Error processing results, attempting revert", "level", executionLevel, "error", errProcessingResults)
			if revertErr := e.revertWorkflow(workflowCtx, executionTaskResults, hostContexts, workflowHostFacts, cfg, executionLevel, recapStats); revertErr != nil {
				return fmt.Errorf("error during graph execution on level %d (%v) and also during revert: %w", executionLevel, errProcessingResults, revertErr)
			}
			return fmt.Errorf("error during graph execution on level %d: %w, tasks reverted", executionLevel, errProcessingResults)
		}

		if len(processedResultsThisLevel) > 0 {
			executionTaskResults[executionLevel] = processedResultsThisLevel
		}

		if levelErrored {
			// Execute handlers before reverting, as handlers should run for any tasks that successfully notified them
			if err := e.executeHandlers(workflowCtx, hostContexts, workflowHostFacts, recapStats, cfg); err != nil {
				logger.Warn("Failed to execute handlers before revert", "error", err.Error())
			}

			if !cfg.Revert {
				return fmt.Errorf("run failed on level %d, revert is disabled", executionLevel)
			}
			logger.Info("Run failed, task reversion required", map[string]interface{}{"level": executionLevel})
			if revertErr := e.revertWorkflow(workflowCtx, executionTaskResults, hostContexts, workflowHostFacts, cfg, executionLevel, recapStats); revertErr != nil {
				return fmt.Errorf("run failed on level %d and also failed during revert: %w", executionLevel, revertErr)
			}
			return fmt.Errorf("run failed on level %d and tasks reverted", executionLevel)
		}
	}

	// Execute handlers after all regular tasks complete
	if err := e.executeHandlers(workflowCtx, hostContexts, workflowHostFacts, recapStats, cfg); err != nil {
		return fmt.Errorf("failed to execute handlers: %w", err)
	}

	e.printPlayRecap(cfg, recapStats)
	return nil
}

// executeHandlers runs all notified handlers across all hosts in the workflow context
func (e *TemporalGraphExecutor) executeHandlers(
	workflowCtx workflow.Context,
	hostContexts map[string]*pkg.HostContext,
	workflowHostFacts map[string]map[string]interface{},
	recapStats map[string]map[string]int,
	cfg *config.Config,
) error {
	logger := workflow.GetLogger(workflowCtx)

	// Check if there are any handlers to execute
	hasHandlers := false
	for hostname, hostCtx := range hostContexts {
		if hostCtx.HandlerTracker != nil {
			notifiedHandlers := hostCtx.HandlerTracker.GetNotifiedHandlers()
			logger.Debug("Checking handlers for host", "host", hostname, "notified_handlers", len(notifiedHandlers))
			if len(notifiedHandlers) > 0 {
				hasHandlers = true
				break
			}
		}
	}

	if !hasHandlers {
		logger.Debug("No handlers to execute", "execution_phase", "handlers")
		return nil
	}

	logger.Info("Running handlers", "execution_phase", "handlers")

	// Execute handlers for each host
	for hostname, hostCtx := range hostContexts {
		if hostCtx.HandlerTracker == nil {
			continue
		}

		notifiedHandlers := hostCtx.HandlerTracker.GetNotifiedHandlers()
		if len(notifiedHandlers) == 0 {
			continue
		}

		logger.Debug("Executing handlers for host", "host", hostname, "handler_count", len(notifiedHandlers))

		for _, handler := range notifiedHandlers {
			handler, ok := handler.(*pkg.Task)
			if !ok {
				// TODO: handle non-task nodes
				common.LogWarn("Skipping non-task node in executeHandlers", map[string]interface{}{"node": handler.String()})
				continue
			}

			// Skip if already executed
			if hostCtx.HandlerTracker.IsExecuted(handler.Params().Name) {
				continue
			}

			// Update the existing host context with current facts from workflow
			// Clear existing facts first, then load from workflow
			hostCtx.Facts = &sync.Map{}
			if currentFacts, ok := workflowHostFacts[hostname]; ok {
				for k, v := range currentFacts {
					hostCtx.Facts.Store(k, v)
				}
			}

			// Create closure for the handler using the existing host context (which has connection info)
			handlerClosure := handler.ConstructClosure(hostCtx, cfg)

			// Execute the handler using the temporal task runner
			// TODO: this assumes a single result
			result := e.Runner.ExecuteTask(workflowCtx, handler, handlerClosure, cfg)

			// Mark the handler as executed
			hostCtx.HandlerTracker.MarkExecuted(handler.Params().Name)

			// Handle temporal-specific fact registration first
			if activityResult, ok := result.ExecutionSpecificOutput.(SpageActivityResult); ok {
				if errFact := processActivityResultAndRegisterFacts(workflowCtx, activityResult, handler.Params().Name, hostname, workflowHostFacts, hostContexts); errFact != nil {
					logger.Error("Handler task reported an error after fact registration", "handler", handler.Params().Name, "host", hostname, "error", errFact)
					// Continue processing - the shared processor will handle the result display
				}
			}

			// Process the handler result using shared logic
			processor := &ResultProcessor{
				ExecutionLevel: -1, // Handlers don't have execution levels
				Logger:         NewTemporalLogger(workflowCtx),
				Config:         cfg,
			}
			processor.ProcessHandlerResult(result, recapStats)
		}
	}

	return nil
}

// Revert implements the GraphExecutor interface for Temporal.
// It requires a workflow context, which must be passed in via the context.Context argument.
func (e *TemporalGraphExecutor) Revert(
	ctx context.Context,
	executedTasksHistory []map[string]chan pkg.GraphNode,
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
	recapStats map[string]map[string]int,
) error {
	logger := workflow.GetLogger(workflowCtx)
	var overallRevertError error
	workflowTaskHistory := make(map[string]map[string]interface{})

	// Build the complete history first from all tasks that were processed.
	for i := 0; i <= failingLevel; i++ {
		tasksOnLevel, exists := executedTasks[i]
		if !exists {
			continue
		}
		for _, taskResult := range tasksOnLevel {
			if taskResult.Closure != nil && taskResult.Closure.HostContext != nil && taskResult.Closure.HostContext.Host != nil {
				hostName := taskResult.Closure.HostContext.Host.Name
				if _, ok := workflowTaskHistory[hostName]; !ok {
					workflowTaskHistory[hostName] = make(map[string]interface{})
				}
				// We only store the output if the task was not skipped and had output.
				if taskResult.Output != nil && taskResult.Status != pkg.TaskStatusSkipped {
					workflowTaskHistory[hostName][taskResult.Task.Params().Name] = taskResult.Output
				}
			}
		}
	}

	// Revert from the failingLevel (or last successfully processed level part of it) down to 0
	for level := failingLevel; level >= 0; level-- {
		tasksToRevertOnLevel, levelExists := executedTasks[level]
		if !levelExists || len(tasksToRevertOnLevel) == 0 {
			logger.Debug("No tasks to revert on level", "level", level)
			continue
		}

		logger.Debug("Reverting tasks for level", "level", level, "num_tasks_to_revert", len(tasksToRevertOnLevel))

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
				logger.Error("Cannot revert task due to nil Closure/HostContext/Host in stored TaskResult", "task_name", originalTask.Params().Name)
				syntheticRevertResult := pkg.TaskResult{
					Task:    originalTask,
					Closure: originalClosure,
					Error:   fmt.Errorf("revert skipped for task %s due to missing context in original result", originalTask.Params().Name),
					Status:  pkg.TaskStatusFailed,
				}
				revertResultsCh.SendAsync(syntheticRevertResult)
				if cfg.ExecutionMode == "parallel" && revertCompletionCh != nil {
					revertCompletionCh.SendAsync(true) // Still need to signal completion for the slot
				}
				continue
			}

			// Quick check: Skip expensive temporal activity for tasks without revert actions
			if P, ok := originalTask.Params().Params.Actual.(interface{ HasRevert() bool }); !ok || !P.HasRevert() {
				logger.Debug("Skipping revert activity for task without revert action", "task", originalTask.Params().Name)
				syntheticRevertResult := pkg.TaskResult{
					Task:    originalTask,
					Closure: originalClosure,
					Error:   nil, // No error, just skipped
					Status:  pkg.TaskStatusOk,
					Failed:  false,
					Changed: false,
				}
				revertResultsCh.SendAsync(syntheticRevertResult)
				if cfg.ExecutionMode == "parallel" && revertCompletionCh != nil {
					revertCompletionCh.SendAsync(true)
				}
				continue
			}

			hostName := originalClosure.HostContext.Host.Name // Capture for goroutine

			// Create a new closure with up-to-date facts for the revert operation.
			// The original closure might have stale facts.
			clonedRevertClosure := &pkg.Closure{
				HostContext: &pkg.HostContext{
					Host:    originalClosure.HostContext.Host, // Use original host definition
					Facts:   &sync.Map{},                      // Fresh facts map for this revert operation
					History: &sync.Map{},
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
				logger.Warn("No current facts found for host during revert, revert task will use minimal facts", "host", hostName, "task", originalTask.Params().Name)
			}
			if historyForHost, ok := workflowTaskHistory[hostName]; ok {
				for taskName, output := range historyForHost {
					clonedRevertClosure.HostContext.History.Store(taskName, output)
				}
			}

			actualRevertsDispatched++
			if cfg.ExecutionMode == "parallel" {
				workflow.Go(workflowCtx, func(revertCtx workflow.Context) {
					if t2, ok2 := originalTask.(*pkg.Task); ok2 {
						revertResult := e.Runner.RevertTask(revertCtx, *t2, clonedRevertClosure, cfg)
						revertResultsCh.SendAsync(revertResult)
						if revertCompletionCh != nil {
							revertCompletionCh.SendAsync(true)
						}
					} else {
						revertResultsCh.SendAsync(pkg.TaskResult{Task: originalTask, Closure: clonedRevertClosure, Status: pkg.TaskStatusOk})
						if revertCompletionCh != nil {
							revertCompletionCh.SendAsync(true)
						}
					}
				})
			} else {
				if t2, ok2 := originalTask.(*pkg.Task); ok2 {
					revertResult := e.Runner.RevertTask(workflowCtx, *t2, clonedRevertClosure, cfg)
					revertResultsCh.SendAsync(revertResult)
				} else {
					revertResultsCh.SendAsync(pkg.TaskResult{Task: originalTask, Closure: clonedRevertClosure, Status: pkg.TaskStatusOk})
				}
			}
		}

		if cfg.ExecutionMode == "parallel" && actualRevertsDispatched > 0 {
			// Ensure revertCompletionCh is not nil before using it
			if revertCompletionCh != nil {
				for i := 0; i < actualRevertsDispatched; i++ {
					revertCompletionCh.Receive(workflowCtx, nil)
				}
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
				taskName := revertResult.Task.Params().Name
				if activityOutput, ok := revertResult.ExecutionSpecificOutput.(SpageActivityResult); ok {
					if errFact := processActivityResultAndRegisterFacts(workflowCtx, activityOutput, "revert-"+taskName, hostName, workflowHostFacts, hostContexts); errFact != nil {
						logger.Error("Reverted task reported an error after fact registration", "task", taskName, "host", hostName, "original_error", errFact)
						levelRevertHardErrored = true
					} else if activityOutput.Error != "" && !activityOutput.Ignored { // Check activityOutput.Error even if errFact is nil, if not ignored
						logger.Error("Revert task failed (reported by activity)", "task", taskName, "host", hostName, "activity_error", activityOutput.Error)
						levelRevertHardErrored = true
					}
				} else if revertResult.Error != nil { // If no SpageActivityResult, check TaskResult.Error
					logger.Error("Revert task failed (TaskResult.Error)", "task", taskName, "host", hostName, "error", revertResult.Error)
					levelRevertHardErrored = true
				}
			} else if revertResult.Error != nil { // Closure or context was nil, but there's an error
				logger.Error("Revert task failed (context missing in original result or other error)", "task_name", revertResult.Task.Params().Name, "error", revertResult.Error)
				levelRevertHardErrored = true
			}
		}

		if levelRevertHardErrored {
			logger.Error("Hard error occurred during revert for level, marking overall revert as failed.", "level", level)
			overallRevertError = fmt.Errorf("revert failed on level %d", level) // Mark that at least one level's revert had issues
		}
	}

	return overallRevertError
}

func (e *TemporalGraphExecutor) printPlayRecap(cfg *config.Config, recapStats map[string]map[string]int) {
	// Use logging instead of fmt.Printf to avoid deadlocks with Temporal's error handling
	if cfg.Logging.Format == "plain" {
		common.LogInfo("PLAY RECAP", map[string]interface{}{})
		for hostname, stats := range recapStats {
			okCount := stats["ok"]
			changedCount := stats["changed"]
			failedCount := stats["failed"]
			skippedCount := stats["skipped"]
			ignoredCount := stats["ignored"]
			common.LogInfo("Host recap", map[string]interface{}{
				"host":    hostname,
				"ok":      okCount,
				"changed": changedCount,
				"failed":  failedCount,
				"skipped": skippedCount,
				"ignored": ignoredCount,
			})
		}
	} else {
		common.LogInfo("Play recap", map[string]interface{}{"stats": recapStats})
	}
}

// SpageTemporalWorkflow defines the main workflow logic.
func SpageTemporalWorkflow(ctx workflow.Context, graphInput *pkg.Graph, inventoryFile string, spageConfigInput *config.Config, limitPattern string) error {
	logger := workflow.GetLogger(ctx)

	temporalRunner := NewTemporalTaskRunner(ctx)
	e := &TemporalGraphExecutor{Runner: *temporalRunner}

	err := pkg.ExecuteGraphWithLimit(e, graphInput, inventoryFile, spageConfigInput, spageConfigInput.GetDaemonReporting(), limitPattern)

	if err != nil {
		logger.Error("SpageTemporalWorkflow failed", "error", err)
		return err
	}

	return nil
}

// RunSpageTemporalWorkerAndWorkflowOptions defines options for RunSpageTemporalWorkerAndWorkflow.
type RunSpageTemporalWorkerAndWorkflowOptions struct {
	Graph            *pkg.Graph
	InventoryPath    string
	LoadedConfig     *config.Config // Changed from ConfigPath to break import cycle with cmd
	WorkflowIDPrefix string
	LimitPattern     string
}

// RunSpageTemporalWorkerAndWorkflow sets up and runs a Temporal worker for Spage tasks,
// and can optionally trigger a workflow execution.
func RunSpageTemporalWorkerAndWorkflow(opts RunSpageTemporalWorkerAndWorkflowOptions) error {
	spageAppConfig := opts.LoadedConfig
	if spageAppConfig == nil {
		common.LogInfo("Warning: No Spage configuration provided to RunSpageTemporalWorkerAndWorkflow. Using a default config.", map[string]interface{}{})
		spageAppConfig = &config.Config{
			ExecutionMode: "parallel",
			Logging:       config.LoggingConfig{Format: "plain", Level: "info"},
			Revert:        true,
			Temporal: config.TemporalConfig{
				Address:          "",
				TaskQueue:        "SPAGE_DEFAULT_TASK_QUEUE",
				WorkflowIDPrefix: "spage-workflow",
			},
		}
	} else {
		common.LogInfo("Using provided Spage configuration.", map[string]interface{}{"config": spageAppConfig})
	}

	clientOpts := client.Options{}
	if spageAppConfig.Temporal.Address != "" {
		clientOpts.HostPort = spageAppConfig.Temporal.Address
		common.LogInfo("Temporal client configured with address", map[string]interface{}{"address": spageAppConfig.Temporal.Address})
	} else {
		common.LogInfo("Temporal client using default address (localhost:7233 or TEMPORAL_GRPC_ENDPOINT).", map[string]interface{}{})
	}

	temporalClient, err := client.Dial(clientOpts)
	if err != nil {
		common.LogError("Unable to create Temporal client", map[string]interface{}{"error": err})
		return err
	}
	defer temporalClient.Close()

	taskQueue := spageAppConfig.Temporal.TaskQueue
	if taskQueue == "" {
		taskQueue = "SPAGE_DEFAULT_TASK_QUEUE"
		common.LogInfo("TaskQueue from config is empty, using emergency default", map[string]interface{}{"task_queue": taskQueue})
	} else {
		common.LogInfo("Using Temporal TaskQueue from config", map[string]interface{}{"task_queue": taskQueue})
	}
	myWorker := worker.New(temporalClient, taskQueue, worker.Options{})

	myWorker.RegisterWorkflow(SpageTemporalWorkflow)
	myWorker.RegisterWorkflow(SpageMetaWorkflow)
	myWorker.RegisterActivity(ExecuteSpageTaskActivity)
	myWorker.RegisterActivity(RevertSpageTaskActivity)         // Register the new activity
	myWorker.RegisterActivity(ExecuteSpageRunOnceLoopActivity) // Register the run_once loop activity

	common.LogInfo("Starting Temporal worker on task queue", map[string]interface{}{"task_queue": taskQueue})
	if err := myWorker.Start(); err != nil {
		common.LogError("Unable to start worker", map[string]interface{}{"error": err})
		return err
	}

	if spageAppConfig.Temporal.Trigger {
		common.LogInfo("Attempting to start the SpageTemporalWorkflow based on configuration...", map[string]interface{}{})
		workflowIDPrefix := opts.WorkflowIDPrefix
		if workflowIDPrefix == "" {
			workflowIDPrefix = "spage-workflow"
			common.LogInfo("WorkflowIDPrefix from config is empty, using emergency default", map[string]interface{}{"workflow_id_prefix": workflowIDPrefix})
		}
		workflowID := workflowIDPrefix + "-" + uuid.New().String()

		workflowOptions := client.StartWorkflowOptions{
			ID:        workflowID,
			TaskQueue: taskQueue,
		}

		common.LogDebug("Executing workflow with graph, inventory, and config.", map[string]interface{}{
			"graph_tasks_count": len(opts.Graph.Nodes),
			"inventory_path":    opts.InventoryPath,
			"config_mode":       spageAppConfig.ExecutionMode,
		})

		we, err := temporalClient.ExecuteWorkflow(context.Background(), workflowOptions, SpageTemporalWorkflow, opts.Graph, opts.InventoryPath, spageAppConfig, opts.LimitPattern)
		if err != nil {
			common.LogError("Unable to execute SpageTemporalWorkflow", map[string]interface{}{"error": err})
			return err
		}
		common.LogInfo("Successfully started SpageTemporalWorkflow", map[string]interface{}{"workflow_id": we.GetID(), "run_id": we.GetRunID()})

		common.LogInfo("Waiting for workflow to complete...", map[string]interface{}{"workflow_id": we.GetID()})
		err = we.Get(context.Background(), nil)
		if err != nil {
			common.LogError("Workflow completed with error", map[string]interface{}{"workflow_id": we.GetID(), "error": err})
			myWorker.Stop()
			return err
		} else {
			common.LogInfo("Workflow completed successfully.", map[string]interface{}{"workflow_id": we.GetID()})
			myWorker.Stop()
		}
	} else {
		common.LogInfo("Application setup complete. Worker is running. Press Ctrl+C to exit.", map[string]interface{}{})
		<-worker.InterruptCh()
		common.LogInfo("Shutting down worker...", map[string]interface{}{"task_queue": taskQueue})
		myWorker.Stop()
		common.LogInfo("Worker stopped.", map[string]interface{}{"task_queue": taskQueue})
	}
	return nil
}

func ExecuteSpageRunOnceLoopActivity(ctx context.Context, input SpageRunOnceLoopActivityInput) (*SpageRunOnceLoopActivityResult, error) {
	logger := activity.GetLogger(ctx)

	activity.RecordHeartbeat(ctx, fmt.Sprintf("Starting run_once loop task %s on host %s with %d iterations", input.TaskDefinition.Name, input.TargetHost.Name, len(input.LoopItems)))

	hostCtx, err := pkg.InitializeHostContext(&input.TargetHost, input.SpageCoreConfig)
	if err != nil {
		logger.Error("Failed to initialize host context for run_once loop", "host", input.TargetHost.Name, "task", input.TaskDefinition.Name, "error", err)
		return &SpageRunOnceLoopActivityResult{
			HostName: input.TargetHost.Name,
			TaskName: input.TaskDefinition.Name,
			LoopResults: []SpageActivityResult{{
				HostName: input.TargetHost.Name,
				TaskName: input.TaskDefinition.Name,
				Error:    fmt.Sprintf("failed to initialize host context for run_once loop task %s: %v", input.TaskDefinition.Name, err),
			}},
		}, nil
	}
	defer func() {
		if closeErr := hostCtx.Close(); closeErr != nil {
			logger.Warn("Failed to close host context for run_once loop", "host", input.TargetHost.Name, "error", closeErr)
		}
	}()

	// Load facts from workflow
	if input.CurrentHostFacts != nil {
		for k, v := range input.CurrentHostFacts {
			hostCtx.Facts.Store(k, v)
		}
	}

	// Initialize handler tracker with handlers from the graph (convert DTOs)
	if len(input.Handlers) > 0 {
		nodes := make([]pkg.GraphNode, 0, len(input.Handlers))
		for _, dto := range input.Handlers {
			if gn := fromGraphNodeDTO(dto); gn != nil {
				nodes = append(nodes, gn)
			}
		}
		hostCtx.InitializeHandlerTracker(nodes)
	} else {
		hostCtx.InitializeHandlerTracker(nil)
	}

	var loopResults []SpageActivityResult

	// Execute each loop iteration
	for i, loopItem := range input.LoopItems {
		closure := input.TaskDefinition.ConstructClosure(hostCtx, input.SpageCoreConfig)

		if loopItem != nil {
			loopVarName := "item"
			closure.ExtraFacts[loopVarName] = loopItem
		}

		// TODO: handle meta tasks
		taskResultCh := input.TaskDefinition.ExecuteModule(closure)
		taskResult := <-taskResultCh
		activity.RecordHeartbeat(ctx, fmt.Sprintf("Finished loop iteration %d for task %s on host %s", i+1, input.TaskDefinition.Name, input.TargetHost.Name))

		result := SpageActivityResult{
			HostName:       input.TargetHost.Name,
			TaskName:       input.TaskDefinition.Name,
			RegisteredVars: make(map[string]interface{}),
		}

		var ignoredError *pkg.IgnoredTaskError
		if errors.As(taskResult.Error, &ignoredError) {
			result.Ignored = true
			originalErr := ignoredError.Unwrap()
			result.Error = originalErr.Error()
			logger.Warn("Loop iteration failed but error was ignored", "task", input.TaskDefinition.Name, "iteration", i, "originalError", originalErr)
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
			if input.TaskDefinition.Register != "" {
				result.RegisteredVars[input.TaskDefinition.Register] = failureMap
			}
		} else if taskResult.Error != nil {
			result.Error = taskResult.Error.Error()
			logger.Error("Loop iteration execution failed", "task", input.TaskDefinition.Name, "iteration", i, "error", taskResult.Error)
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
				if converted, ok := pkg.ConvertOutputToFactsMap(taskResult.Output).(map[string]interface{}); ok {
					result.ModuleOutputMap = converted
				}
			} else {
				result.Skipped = true
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

		// Capture handler notifications for this loop iteration
		if hostCtx.HandlerTracker != nil {
			notifiedHandlerTasks := hostCtx.HandlerTracker.GetNotifiedHandlers()
			result.NotifiedHandlers = make([]string, len(notifiedHandlerTasks))
			for j, handler := range notifiedHandlerTasks {
				result.NotifiedHandlers[j] = handler.Params().Name
			}
		}

		loopResults = append(loopResults, result)
	}

	// Capture all facts from the activity's HostContext after all loop iterations
	hostFactsSnapshot := make(map[string]interface{})

	// Create a map of task-level variable names to exclude from persistence
	taskLevelVars := make(map[string]bool)
	if input.TaskDefinition.Vars != nil {
		if varsMap, ok := input.TaskDefinition.Vars.(map[string]interface{}); ok {
			for varName := range varsMap {
				taskLevelVars[varName] = true
			}
		}
	}

	hostCtx.Facts.Range(func(key, value interface{}) bool {
		if kStr, ok := key.(string); ok {
			// Only capture facts that are NOT task-level variables
			if !taskLevelVars[kStr] {
				hostFactsSnapshot[kStr] = value
			}
		}
		return true
	})

	return &SpageRunOnceLoopActivityResult{
		HostName:          input.TargetHost.Name,
		TaskName:          input.TaskDefinition.Name,
		LoopResults:       loopResults,
		HostFactsSnapshot: hostFactsSnapshot,
	}, nil
}

func processActivityResultAndRegisterFacts(
	ctx workflow.Context,
	activityResult SpageActivityResult,
	taskName string,
	hostName string,
	hostFacts map[string]map[string]interface{},
	hostContexts map[string]*pkg.HostContext,
) error {
	// Reduce concurrent logging to avoid deadlocks - only log and return error, don't do both
	if activityResult.Error != "" && !activityResult.Ignored {
		return fmt.Errorf("task '%s' on host '%s' failed: %s", taskName, hostName, activityResult.Error)
	}

	logger := workflow.GetLogger(ctx)

	if _, ok := hostFacts[activityResult.HostName]; !ok {
		logger.Warn("Host facts map not found for host, creating new one.", "host", activityResult.HostName)
		hostFacts[activityResult.HostName] = make(map[string]interface{})
	}

	if activityResult.HostFactsSnapshot != nil {
		for key, value := range activityResult.HostFactsSnapshot {
			hostFacts[activityResult.HostName][key] = value
		}
	}

	if len(activityResult.RegisteredVars) > 0 {
		for key, value := range activityResult.RegisteredVars {
			hostFacts[activityResult.HostName][key] = value
		}
	}

	if len(activityResult.NotifiedHandlers) > 0 {
		if hostCtx, exists := hostContexts[activityResult.HostName]; exists && hostCtx.HandlerTracker != nil {
			logger.Debug("Applying handler notifications from activity to workflow context", "host", activityResult.HostName, "task", taskName, "notified_handlers", activityResult.NotifiedHandlers)
			for _, handlerName := range activityResult.NotifiedHandlers {
				hostCtx.HandlerTracker.NotifyHandler(handlerName)
			}
		}
	}

	return nil
}

// SpageMetaWorkflowInput defines the input for executing a MetaTask as a child workflow
type SpageMetaWorkflowInput struct {
	Meta             GraphNodeDTO
	TargetHost       pkg.Host
	CurrentHostFacts map[string]interface{}
	SpageCoreConfig  *config.Config
	Handlers         []GraphNodeDTO
}

// SpageMetaWorkflowResult summarizes the child workflow execution
type SpageMetaWorkflowResult struct {
	Changed           bool
	Error             string
	HostFactsSnapshot map[string]interface{}
	NotifiedHandlers  []string
}

// SpageMetaWorkflow executes a MetaTask's children sequentially for a single host
func SpageMetaWorkflow(ctx workflow.Context, input SpageMetaWorkflowInput) (SpageMetaWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)

	// Initialize host context
	hostCtx, err := pkg.InitializeHostContext(&input.TargetHost, input.SpageCoreConfig)
	if err != nil {
		return SpageMetaWorkflowResult{Changed: false, Error: fmt.Sprintf("failed to initialize host context: %v", err)}, nil
	}
	defer func() {
		if closeErr := hostCtx.Close(); closeErr != nil {
			logger.Warn("Failed to close host context in MetaWorkflow", "host", input.TargetHost.Name, "error", closeErr)
		}
	}()

	// Seed facts
	if input.CurrentHostFacts != nil {
		for k, v := range input.CurrentHostFacts {
			hostCtx.Facts.Store(k, v)
		}
	}

	// Initialize handlers on this host
	if len(input.Handlers) > 0 {
		nodes := make([]pkg.GraphNode, 0, len(input.Handlers))
		for _, dto := range input.Handlers {
			if gn := fromGraphNodeDTO(dto); gn != nil {
				nodes = append(nodes, gn)
			}
		}
		hostCtx.InitializeHandlerTracker(nodes)
	} else {
		hostCtx.InitializeHandlerTracker(nil)
	}

	// Reconstruct the meta task from DTO
	metaNode := fromGraphNodeDTO(input.Meta)
	meta, ok := metaNode.(*pkg.MetaTask)
	if !ok || meta == nil {
		return SpageMetaWorkflowResult{Changed: false, Error: "invalid meta input"}, nil
	}

	changed := false
	blockFailed := false

	// Execute children sequentially
	for _, child := range meta.Children {
		closure := (&pkg.Task{TaskParams: &pkg.TaskParams{}}).ConstructClosure(hostCtx, input.SpageCoreConfig)
		res := NewTemporalTaskRunner(ctx).ExecuteTask(ctx, child, closure, input.SpageCoreConfig)
		if activityResult, ok := res.ExecutionSpecificOutput.(SpageActivityResult); ok {
			if activityResult.HostFactsSnapshot != nil {
				for k, v := range activityResult.HostFactsSnapshot {
					hostCtx.Facts.Store(k, v)
				}
			}
			if len(activityResult.RegisteredVars) > 0 {
				for k, v := range activityResult.RegisteredVars {
					hostCtx.Facts.Store(k, v)
				}
			}
			if hostCtx.HandlerTracker != nil {
				for _, name := range activityResult.NotifiedHandlers {
					hostCtx.HandlerTracker.NotifyHandler(name)
				}
			}
		}
		if res.Status == pkg.TaskStatusFailed {
			blockFailed = true
		}
		if res.Status == pkg.TaskStatusChanged || res.Changed {
			changed = true
		}
	}

	// If failed, run rescue
	if blockFailed {
		rescueFailed := false
		for _, rNode := range meta.Rescue {
			rClosure := (&pkg.Task{TaskParams: &pkg.TaskParams{}}).ConstructClosure(hostCtx, input.SpageCoreConfig)
			rRes := NewTemporalTaskRunner(ctx).ExecuteTask(ctx, rNode, rClosure, input.SpageCoreConfig)
			if activityResult, ok := rRes.ExecutionSpecificOutput.(SpageActivityResult); ok {
				if activityResult.HostFactsSnapshot != nil {
					for k, v := range activityResult.HostFactsSnapshot {
						hostCtx.Facts.Store(k, v)
					}
				}
				if len(activityResult.RegisteredVars) > 0 {
					for k, v := range activityResult.RegisteredVars {
						hostCtx.Facts.Store(k, v)
					}
				}
				if hostCtx.HandlerTracker != nil {
					for _, name := range activityResult.NotifiedHandlers {
						hostCtx.HandlerTracker.NotifyHandler(name)
					}
				}
			}
			if rRes.Status == pkg.TaskStatusFailed {
				rescueFailed = true
			}
			if rRes.Status == pkg.TaskStatusChanged || rRes.Changed {
				changed = true
			}
		}
		if !rescueFailed {
			blockFailed = false
		}
	}

	// Always
	for _, aNode := range meta.Always {
		aClosure := (&pkg.Task{TaskParams: &pkg.TaskParams{}}).ConstructClosure(hostCtx, input.SpageCoreConfig)
		aRes := NewTemporalTaskRunner(ctx).ExecuteTask(ctx, aNode, aClosure, input.SpageCoreConfig)
		if activityResult, ok := aRes.ExecutionSpecificOutput.(SpageActivityResult); ok {
			if activityResult.HostFactsSnapshot != nil {
				for k, v := range activityResult.HostFactsSnapshot {
					hostCtx.Facts.Store(k, v)
				}
			}
			if len(activityResult.RegisteredVars) > 0 {
				for k, v := range activityResult.RegisteredVars {
					hostCtx.Facts.Store(k, v)
				}
			}
			if hostCtx.HandlerTracker != nil {
				for _, name := range activityResult.NotifiedHandlers {
					hostCtx.HandlerTracker.NotifyHandler(name)
				}
			}
		}
		if aRes.Status == pkg.TaskStatusChanged || aRes.Changed {
			changed = true
		}
	}

	snapshot := make(map[string]interface{})
	hostCtx.Facts.Range(func(key, value interface{}) bool {
		if ks, ok := key.(string); ok {
			snapshot[ks] = value
		}
		return true
	})
	var notified []string
	if hostCtx.HandlerTracker != nil {
		for _, h := range hostCtx.HandlerTracker.GetNotifiedHandlers() {
			notified = append(notified, h.Params().Name)
		}
	}
	var errMsg string
	if blockFailed {
		errMsg = "block failed"
	}
	return SpageMetaWorkflowResult{Changed: changed, Error: errMsg, HostFactsSnapshot: snapshot, NotifiedHandlers: notified}, nil
}
