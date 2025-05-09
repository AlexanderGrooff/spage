package executor

import (
	"context"
	"errors"
	"fmt"
	"github.com/AlexanderGrooff/spage/pkg"
	"log"
	"time"

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
// differences in fact/state management compared to BaseExecutor's assumptions.
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
func (r *TemporalTaskRunner) RunTask(ctx context.Context, task pkg.Task, closure *pkg.Closure, cfg *config.Config) pkg.TaskResult {
	// ctx is the parent context from the caller of RunTask (e.g., workflow.Background(r.WorkflowCtx)).
	// r.WorkflowCtx is the actual workflow context for Temporal operations.
	logger := workflow.GetLogger(r.WorkflowCtx)

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
	// Use r.WorkflowCtx for Temporal API calls like WithActivityOptions and ExecuteActivity
	temporalAwareCtx := workflow.WithActivityOptions(r.WorkflowCtx, ao)

	startTime := time.Now() // Record start time before executing activity
	future := workflow.ExecuteActivity(temporalAwareCtx, ExecuteSpageTaskActivity, activityInput)
	errOnGet := future.Get(r.WorkflowCtx, &activityOutput)
	duration := time.Since(startTime) // Calculate duration after activity completion or error

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

// SpageTemporalWorkflow defines the main workflow logic.
func SpageTemporalWorkflow(ctx workflow.Context, graphInput *pkg.Graph, inventoryInput *pkg.Inventory, spageConfigInput *config.Config) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("SpageTemporalWorkflow started", "workflowId", workflow.GetInfo(ctx).WorkflowExecution.ID)

	// Activity options are now set within TemporalTaskRunner.RunTask or globally if preferred.
	// If set globally for the workflow context:
	// ao := workflow.ActivityOptions{...}
	// ctx = workflow.WithActivityOptions(ctx, ao)
	// However, TemporalTaskRunner will apply its own for now.

	hostFacts := make(map[string]map[string]interface{})
	if inventoryInput != nil {
		for hostName, host := range inventoryInput.Hosts {
			hostFacts[hostName] = inventoryInput.GetInitialFactsForHost(host)
			logger.Debug("Initialized facts for host in workflow", "host", hostName, "facts_count", len(hostFacts[hostName]))
		}
	}

	var orderedLevelsOfTasks [][]pkg.Task
	if spageConfigInput.ExecutionMode == "sequential" {
		orderedLevelsOfTasks = graphInput.SequentialTasks()
	} else {
		orderedLevelsOfTasks = graphInput.ParallelTasks()
	}

	// Instantiate the runner once for the workflow context
	temporalRunner := NewTemporalTaskRunner(ctx)

	for levelIdx, tasksInThisLevel := range orderedLevelsOfTasks {
		logger.Info("Processing graph level", "level", levelIdx, "mode", spageConfigInput.ExecutionMode, "task_count_in_level", len(tasksInThisLevel))

		targetHosts := inventoryInput.Hosts // Assuming inventoryInput is not nil after initial checks/defaults
		if targetHosts == nil {
			targetHosts = make(map[string]*pkg.Host)
		} // Safety for nil inventory

		if spageConfigInput.ExecutionMode == "sequential" {
			logger.Info("Executing level sequentially", "level", levelIdx)
			for _, currentTaskDefinition := range tasksInThisLevel {
				for hostName, host := range targetHosts {
					loopItems, err := getLoopItemsForTask(ctx, currentTaskDefinition, host, hostFacts[hostName])
					if err != nil {
						return err
					} // Propagate error from loop parsing

					for _, loopItem := range loopItems {
						// Construct Closure for this specific task run
						tempHostCtx, err := pkg.InitializeHostContext(host) // Fresh HostContext for closure
						if err != nil {
							return fmt.Errorf("failed to init temp HostContext for closure: %w", err)
						}
						currentFactsForClosure := hostFacts[hostName]
						if currentFactsForClosure != nil {
							for k, v := range currentFactsForClosure {
								tempHostCtx.Facts.Store(k, v)
							}
						}
						extraFactsForClosure := make(map[string]interface{})
						if loopItem != nil {
							extraFactsForClosure["item"] = loopItem
						}

						closureForRunner := &pkg.Closure{
							HostContext: tempHostCtx,
							ExtraFacts:  extraFactsForClosure,
						}

						// Pass context.Background() for the first ctx argument of RunTask, as
						// TemporalTaskRunner.RunTask uses its internally stored r.WorkflowCtx for actual Temporal operations,
						// and the first context.Context param is currently not utilized by it for activity logic.
						taskRunResult := temporalRunner.RunTask(context.Background(), currentTaskDefinition, closureForRunner, spageConfigInput)
						tempHostCtx.Close() // Close the temporary host context

						var activityResult SpageActivityResult
						if taskRunResult.ExecutionSpecificOutput != nil {
							var castOk bool
							activityResult, castOk = taskRunResult.ExecutionSpecificOutput.(SpageActivityResult)
							if !castOk {
								errMsg := fmt.Sprintf("failed to cast ExecutionSpecificOutput to SpageActivityResult for task %s on host %s", currentTaskDefinition.Name, hostName)
								logger.Error(errMsg)
								return errors.New(errMsg)
							}
						} else if taskRunResult.Error != nil { // If ExecutionSpecificOutput is nil but there was an error (e.g. future.Get() failed)
							// Populate a minimal SpageActivityResult for processActivityResultAndRegisterFacts
							activityResult = SpageActivityResult{
								HostName: hostName,
								TaskName: currentTaskDefinition.Name,
								Error:    taskRunResult.Error.Error(), // Use the error from TaskResult
								// Ignored needs to be derived if possible. Assuming not ignored if we are in this path from direct error.
							}
							var ignoredErr *pkg.IgnoredTaskError
							if errors.As(taskRunResult.Error, &ignoredErr) {
								activityResult.Ignored = true
							}
						}

						errProcess := processActivityResultAndRegisterFacts(ctx, activityResult, currentTaskDefinition.Name, hostName, hostFacts)
						if errProcess != nil {
							return errProcess // This is a hard failure reported by the activity processing logic
						}
					}
				}
			}
		} else { // Parallel execution for the level
			logger.Info("Executing level in parallel", "level", levelIdx)
			var activityFutures []workflow.Future
			type futureInfo struct {
				TaskName string
				HostName string
			}
			var futureInfos []futureInfo

			numDispatched := 0

			for _, currentTaskDefinition := range tasksInThisLevel {
				for hostName, host := range targetHosts {
					loopItems, err := getLoopItemsForTask(ctx, currentTaskDefinition, host, hostFacts[hostName])
					if err != nil {
						return err
					}

					for _, loopItem := range loopItems {
						numDispatched++
						// Constructing SpageActivityInput directly for ExecuteActivity in parallel loop.
						// tempHostCtx and extraFactsForClosure are used to build activityInput below.
						tempHostCtx, err := pkg.InitializeHostContext(host)
						if err != nil {
							return fmt.Errorf("failed to init temp HostContext for closure (parallel): %w", err)
						}
						currentFactsForClosure := hostFacts[hostName]
						if currentFactsForClosure != nil {
							for k, v := range currentFactsForClosure {
								tempHostCtx.Facts.Store(k, v)
							}
						}

						activityInput := SpageActivityInput{
							TaskDefinition:   currentTaskDefinition,
							TargetHost:       *host,
							LoopItem:         loopItem, // loopItem used directly
							CurrentHostFacts: currentFactsForClosure,
							SpageCoreConfig:  spageConfigInput,
						}
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
						temporalAwareCtx := workflow.WithActivityOptions(ctx, ao)
						future := workflow.ExecuteActivity(temporalAwareCtx, ExecuteSpageTaskActivity, activityInput)
						activityFutures = append(activityFutures, future)
						futureInfos = append(futureInfos, futureInfo{TaskName: currentTaskDefinition.Name, HostName: hostName})

						tempHostCtx.Close() // Close after facts have been potentially copied/used by activityInput logic (or if activity is truly fire and forget about hostCtx itself)
					}
				}
			}

			for i := 0; i < numDispatched; i++ {
				var activityResult SpageActivityResult
				// Wait for any of the futures to complete
				// This is a simplified model; for true parallelism, futures would be collected without blocking sequentially on Get.
				// The original code collected all futures then iterated .Get() - that's better.
				f := activityFutures[i] // This sequential get is not truly parallel waiting
				info := futureInfos[i]
				errOnGet := f.Get(ctx, &activityResult)

				if errOnGet != nil {
					logger.Error("Activity future.Get() failed (parallel mode)", "task", info.TaskName, "host", info.HostName, "error", errOnGet)
					return fmt.Errorf("activity %s on host %s failed to complete: %w", info.TaskName, info.HostName, errOnGet)
				}

				errProcess := processActivityResultAndRegisterFacts(ctx, activityResult, info.TaskName, info.HostName, hostFacts)
				if errProcess != nil {
					return errProcess
				}
			}
		}
		logger.Info("Completed processing graph level", "level", levelIdx)
	}

	logger.Info("SpageTemporalWorkflow completed successfully.")
	return nil
}

// Helper to encapsulate loop parsing logic for workflow
func getLoopItemsForTask(ctx workflow.Context, task pkg.Task, host *pkg.Host, currentHostFacts map[string]interface{}) ([]interface{}, error) {
	logger := workflow.GetLogger(ctx)
	loopItems := []interface{}{nil} // Default: run once if no loop
	isLoopTask := false

	if task.Loop != nil {
		// Need a temporary HostContext to use ParseLoop, as ParseLoop expects it for fact lookups.
		tempHostCtxForLoop, err := pkg.InitializeHostContext(host)
		if err != nil {
			logger.Error("Failed to initialize temporary host context for loop parsing", "task", task.Name, "host", host.Name, "error", err)
			return nil, fmt.Errorf("failed to init temp HostContext for loop parsing for task %s on host %s: %w", task.Name, host.Name, err)
		}
		defer tempHostCtxForLoop.Close()

		if currentHostFacts != nil {
			for k, v := range currentHostFacts {
				tempHostCtxForLoop.Facts.Store(k, v)
			}
		}

		parsedLoopItems, err := pkg.ParseLoop(task, tempHostCtxForLoop) // ParseLoop is from executor_utils.go
		if err != nil {
			logger.Error("Failed to parse loop for task", "task", task.Name, "host", host.Name, "error", err)
			return nil, fmt.Errorf("failed to parse loop for task %s on host %s: %w", task.Name, host.Name, err)
		}
		if len(parsedLoopItems) > 0 {
			loopItems = parsedLoopItems
			isLoopTask = true
		}
	}
	logger.Debug("Task loop processing details", "task", task.Name, "host", host.Name, "isLoop", isLoopTask, "itemCount", len(loopItems))
	return loopItems, nil
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

	// Use the provided LoadedConfig first
	spageAppConfig := opts.LoadedConfig
	if spageAppConfig == nil {
		log.Println("Warning: No Spage configuration provided to RunSpageTemporalWorkerAndWorkflow. Using a default config.")
		// Define a minimal default configuration if none is passed
		spageAppConfig = &config.Config{
			ExecutionMode: "parallel",                                           // Default execution mode
			Logging:       config.LoggingConfig{Format: "plain", Level: "info"}, // Default logging
			Temporal: config.TemporalConfig{ // Default Temporal config
				Address:          "", // SDK default (localhost:7233 or TEMPORAL_GRPC_ENDPOINT)
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
		spageAppInventory, err = pkg.LoadInventory(opts.InventoryPath) // Assumes LoadInventory is in package pkg
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
	if taskQueue == "" { // Should ideally not happen if defaults are set
		taskQueue = "SPAGE_DEFAULT_TASK_QUEUE" // Fallback just in case
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

	if spageAppConfig.Temporal.Trigger { // Use the Trigger field from the loaded configuration
		log.Println("Attempting to start the SpageTemporalWorkflow based on configuration...")
		workflowIDPrefix := spageAppConfig.Temporal.WorkflowIDPrefix
		if workflowIDPrefix == "" { // Should ideally not happen
			workflowIDPrefix = "spage-workflow" // Fallback
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

		// Wait for the workflow to complete
		log.Println("Waiting for workflow to complete...", "WorkflowID", we.GetID())
		err = we.Get(context.Background(), nil) // Wait for the workflow to complete.
		if err != nil {
			log.Fatalf("Workflow %s completed with error: %v", we.GetID(), err)
		} else {
			log.Println("Workflow completed successfully.", "WorkflowID", we.GetID())
			myWorker.Stop()
		}
	} else {
		// If not triggering a workflow, keep the worker running until interrupted
		log.Println("Application setup complete. Worker is running. Press Ctrl+C to exit.")
		<-worker.InterruptCh()
		log.Println("Shutting down worker...")
		myWorker.Stop()
		log.Println("Worker stopped.")
	}
}
