package pkg

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	// "github.com/AlexanderGrooff/spage/cmd" // Removed to break import cycle
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
	TaskDefinition   Task
	TargetHost       Host
	LoopItem         interface{} // nil if not a loop task or for the main item
	CurrentHostFacts map[string]interface{}
	SpageCoreConfig  *config.Config // Pass necessary config parts
}

// SpageActivityResult defines the output from our generic Spage task activity.
type SpageActivityResult struct {
	HostName       string
	TaskName       string
	Output         string
	Changed        bool
	Error          string // Store error message if any
	Skipped        bool
	Ignored        bool
	RegisteredVars map[string]interface{}
}

// ExecuteSpageTaskActivity is the generic activity that runs a Spage task.
func ExecuteSpageTaskActivity(ctx context.Context, input SpageActivityInput) (*SpageActivityResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("ExecuteSpageTaskActivity started", "task", input.TaskDefinition.Name, "host", input.TargetHost.Name)
	activity.RecordHeartbeat(ctx, fmt.Sprintf("Starting task %s on host %s", input.TaskDefinition.Name, input.TargetHost.Name))

	hostCtx, err := InitializeHostContext(&input.TargetHost)
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

	closure := ConstructClosure(hostCtx, input.TaskDefinition) // Assumes ConstructClosure is in package pkg

	if input.LoopItem != nil {
		loopVarName := "item"
		// if input.TaskDefinition.LoopControl.LoopVar != "" { // Temporarily commented out
		// 	loopVarName = input.TaskDefinition.LoopControl.LoopVar
		// }
		closure.ExtraFacts[loopVarName] = input.LoopItem
		logger.Debug("Loop item added to closure facts", "loopVar", loopVarName, "value", input.LoopItem)
	}

	whenCondition := input.TaskDefinition.When
	if whenCondition != "" {
		renderedWhen, err := EvaluateExpression(whenCondition, closure) // Use EvaluateExpression
		if err != nil {
			logger.Error("Failed to evaluate 'when' condition string", "task", input.TaskDefinition.Name, "error", err)
			return &SpageActivityResult{
				HostName: input.TargetHost.Name,
				TaskName: input.TaskDefinition.Name,
				Error:    fmt.Sprintf("failed to render 'when' condition: %v", err),
			}, nil
		}
		shouldRun := IsExpressionTruthy(renderedWhen) // Use IsExpressionTruthy to get boolean
		if !shouldRun {
			logger.Info("Task skipped due to 'when' condition not met", "task", input.TaskDefinition.Name)
			return &SpageActivityResult{
				HostName: input.TargetHost.Name,
				TaskName: input.TaskDefinition.Name,
				Skipped:  true,
			}, nil
		}
	}

	taskResult := input.TaskDefinition.ExecuteModule(closure)
	activity.RecordHeartbeat(ctx, fmt.Sprintf("Finished task %s on host %s", input.TaskDefinition.Name, input.TargetHost.Name))

	result := &SpageActivityResult{
		HostName:       input.TargetHost.Name,
		TaskName:       input.TaskDefinition.Name,
		RegisteredVars: make(map[string]interface{}),
	}

	var ignoredError *IgnoredTaskError // Assumes IgnoredTaskError is in package pkg
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
			if factProvider, ok := taskResult.Output.(FactProvider); ok { // Assumes FactProvider is in package pkg
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
			if factProvider, ok := taskResult.Output.(FactProvider); ok {
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
				valueToStore := ConvertOutputToFactsMap(taskResult.Output) // Assumes ConvertOutputToFactsMap is in package pkg
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
	return result, nil
}

// SpageTemporalWorkflow defines the main workflow logic.
func SpageTemporalWorkflow(ctx workflow.Context, graphInput Graph, inventoryInput *Inventory, spageConfigInput config.Config) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("SpageTemporalWorkflow started", "workflowId", workflow.GetInfo(ctx).WorkflowExecution.ID)

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
	ctx = workflow.WithActivityOptions(ctx, ao)

	hostFacts := make(map[string]map[string]interface{})
	if inventoryInput != nil { // Ensure inventoryInput is not nil before ranging
		for hostName, host := range inventoryInput.Hosts {
			// Use the new method from inventory.go to get a rich set of initial facts
			hostFacts[hostName] = inventoryInput.GetInitialFactsForHost(host) // GetInitialFactsForHost is on Inventory type
			logger.Debug("Initialized facts for host in workflow", "host", hostName, "facts_count", len(hostFacts[hostName]))
		}
	}

	for levelIdx, taskNodesInLevel := range graphInput.Tasks {
		logger.Info("Processing graph level", "level", levelIdx)
		var activityFutures []workflow.Future
		type activityContextInfo struct {
			HostName string
			TaskName string
		}
		var futureContexts []activityContextInfo

		for _, graphNode := range taskNodesInLevel {
			task, ok := graphNode.(Task)
			if !ok {
				if tn, okTn := graphNode.(TaskNode); okTn {
					task = tn.Task
				} else {
					logger.Error("Unexpected node type in graph tasks", "type", fmt.Sprintf("%T", graphNode))
					return fmt.Errorf("unexpected node type %T at level %d", graphNode, levelIdx)
				}
			}

			var targetHosts map[string]*Host
			if inventoryInput != nil {
				targetHosts = inventoryInput.Hosts
			} else {
				targetHosts = make(map[string]*Host) // Avoid nil panic if inventory is nil
				logger.Warn("Workflow received a nil inventoryInput, no hosts to target.")
			}

			for hostName, host := range targetHosts {
				loopItems := []interface{}{nil}
				isLoopTask := false

				if task.Loop != nil {
					tempHostCtx, err := InitializeHostContext(host) // Still needed for ParseLoop if it relies on HostContext methods directly
					if err != nil {
						logger.Error("Failed to initialize temporary host context for loop", "task", task.Name, "host", hostName, "error", err)
						return fmt.Errorf("failed to initialize host context for loop processing for task %s on host %s: %w", task.Name, hostName, err)
					}
					// Populate tempHostCtx.Facts for ParseLoop, using the already compiled hostFacts from the workflow for this host.
					currentInitialFactsForHost, factsExist := hostFacts[hostName]
					if factsExist {
						for k, v := range currentInitialFactsForHost {
							tempHostCtx.Facts.Store(k, v)
						}
					}
					// Note: host.Vars are already part of currentInitialFactsForHost due to GetInitialFactsForHost logic

					parsedLoopItems, err := ParseLoop(task, tempHostCtx) // Assumes ParseLoop is in package pkg
					tempHostCtx.Close()

					if err != nil {
						logger.Error("Failed to parse loop for task", "task", task.Name, "host", hostName, "error", err)
						return fmt.Errorf("failed to parse loop for task %s on host %s: %w", task.Name, hostName, err)
					}
					if len(parsedLoopItems) > 0 {
						loopItems = parsedLoopItems
						isLoopTask = true
					}
				}
				logger.Debug("Task loop processing", "task", task.Name, "host", hostName, "isLoop", isLoopTask, "itemCount", len(loopItems))

				for _, loopItem := range loopItems {
					currentFactsForActivity, factsOk := hostFacts[hostName] // These are the rich initial facts
					if !factsOk {
						// This implies hostName was in inventory.Hosts but not in hostFacts, should not happen if initialized correctly
						logger.Warn("Host facts not found for host during activity scheduling, using empty map.", "host", hostName)
						currentFactsForActivity = make(map[string]interface{})
					}
					activityInput := SpageActivityInput{
						TaskDefinition:   task,
						TargetHost:       *host,
						LoopItem:         loopItem,
						CurrentHostFacts: currentFactsForActivity, // Pass the rich initial facts
						SpageCoreConfig:  &spageConfigInput,
					}
					future := workflow.ExecuteActivity(ctx, ExecuteSpageTaskActivity, activityInput)
					activityFutures = append(activityFutures, future)
					futureContexts = append(futureContexts, activityContextInfo{HostName: hostName, TaskName: task.Name})
				}
			}
		}

		for i, future := range activityFutures {
			var activityResult SpageActivityResult
			err := future.Get(ctx, &activityResult)
			futCtx := futureContexts[i]

			if err != nil {
				logger.Error("Activity future.Get() failed", "task", futCtx.TaskName, "host", futCtx.HostName, "error", err)
				return fmt.Errorf("activity %s on host %s failed to complete: %w", futCtx.TaskName, futCtx.HostName, err)
			}

			if activityResult.Error != "" && !activityResult.Ignored {
				logger.Error("Task failed as reported by activity", "task", activityResult.TaskName, "host", activityResult.HostName, "reportedError", activityResult.Error)
				return fmt.Errorf("task '%s' on host '%s' failed: %s", activityResult.TaskName, activityResult.HostName, activityResult.Error)
			}
			if activityResult.Error != "" && activityResult.Ignored {
				logger.Warn("Task failed but was ignored", "task", activityResult.TaskName, "host", activityResult.HostName, "reportedError", activityResult.Error)
			}

			if activityResult.Skipped {
				logger.Info("Task skipped", "task", activityResult.TaskName, "host", activityResult.HostName)
			} else if !activityResult.Ignored {
				status := "ok"
				if activityResult.Changed {
					status = "changed"
				}
				logger.Info("Task completed", "task", activityResult.TaskName, "host", activityResult.HostName, "status", status)
			}

			// Merge registered variables from activityResult back into the workflow's hostFacts for this host
			if len(activityResult.RegisteredVars) > 0 {
				if _, ok := hostFacts[activityResult.HostName]; !ok {
					// Should not happen if hostFacts was initialized for all hosts from inventory
					logger.Warn("Host facts map not found for host when registering variables, creating new one.", "host", activityResult.HostName)
					hostFacts[activityResult.HostName] = make(map[string]interface{})
				}
				for key, value := range activityResult.RegisteredVars {
					hostFacts[activityResult.HostName][key] = value
					logger.Info("Fact registered by workflow from activity result", "host", activityResult.HostName, "variable", key)
				}
			}
		}
		logger.Info("Completed processing graph level", "level", levelIdx)
	}

	logger.Info("SpageTemporalWorkflow completed successfully.")
	return nil
}

// RunSpageTemporalWorkerAndWorkflowOptions defines options for RunSpageTemporalWorkerAndWorkflow.
type RunSpageTemporalWorkerAndWorkflowOptions struct {
	Graph         Graph
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

	var spageAppInventory *Inventory
	if opts.InventoryPath != "" {
		spageAppInventory, err = LoadInventory(opts.InventoryPath) // Assumes LoadInventory is in package pkg
		if err != nil {
			log.Fatalf("Failed to load Spage inventory file '%s': %v", opts.InventoryPath, err)
		}
		log.Printf("Spage inventory loaded from '%s'.", opts.InventoryPath)
	} else {
		log.Println("No Spage inventory file specified. Creating a default localhost inventory.")
		spageAppInventory = &Inventory{
			Hosts: map[string]*Host{
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

		we, err := temporalClient.ExecuteWorkflow(context.Background(), workflowOptions, SpageTemporalWorkflow, opts.Graph, spageAppInventory, *spageAppConfig)
		if err != nil {
			log.Fatalf("Unable to execute SpageTemporalWorkflow: %v", err)
		}
		log.Println("Successfully started SpageTemporalWorkflow", "WorkflowID", we.GetID(), "RunID", we.GetRunID())
	}

	log.Println("Application setup complete. Worker is running. Press Ctrl+C to exit.")
	<-worker.InterruptCh()
	log.Println("Shutting down worker...")
	myWorker.Stop()
	log.Println("Worker stopped.")
}
