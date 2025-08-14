package pkg

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/AlexanderGrooff/spage/pkg/daemon"
)

// GenericOutput is a flexible map-based implementation of pkg.ModuleOutput.
type GenericOutput map[string]interface{}

// Facts returns the output map itself, so all keys become facts.
func (g GenericOutput) Facts() map[string]interface{} {
	return g
}

// Changed checks for a "changed" key in the map.
func (g GenericOutput) Changed() bool {
	changed, ok := g["changed"].(bool)
	return ok && changed
}

// String provides a simple string representation of the map.
func (g GenericOutput) String() string {
	return fmt.Sprintf("%v", map[string]interface{}(g))
}

// TaskRunner defines an interface for how a single task is executed.
// This allows the core execution logic to be generic, while the actual
// task dispatch (local, Temporal activity, etc.) can be specific.
type TaskRunner interface {
	ExecuteTask(ctx context.Context, task Task, closure *Closure, cfg *config.Config) TaskResult
	RevertTask(ctx context.Context, task Task, closure *Closure, cfg *config.Config) TaskResult
}

// GraphExecutor defines the interface for running a Spage graph.
type GraphExecutor interface {
	Execute(hostContexts map[string]*HostContext, orderedGraph [][]Task, cfg *config.Config) error
	Revert(ctx context.Context, executedTasks []map[string]chan Task, hostContexts map[string]*HostContext, cfg *config.Config) error
}

func ChangeCWDToPlaybookDir(playbookPath string) string {
	basePath := filepath.Dir(playbookPath)
	currCwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current working directory: %v", err)
	}

	if err := os.Chdir(basePath); err != nil {
		log.Fatalf("Failed to change directory to %s: %v", basePath, err)
	}

	common.LogDebug("Changed directory to playbook directory", map[string]interface{}{"path": basePath, "playbook": playbookPath})
	return currCwd
}

func ExecuteGraph(executor GraphExecutor, graph *Graph, inventoryFile string, cfg *config.Config, daemonClientInterface interface{}) error {
	var inventory *Inventory
	var err error

	if inventoryFile != "" {
		// Explicit inventory file provided
		inventory, err = LoadInventory(inventoryFile)
	} else if cfg != nil && cfg.Inventory != "" {
		// No explicit inventory file but inventory paths configured
		inventory, err = LoadInventoryWithPaths("", cfg.Inventory, ".")
	} else {
		// No inventory file and no inventory paths, fall back to default
		inventory, err = LoadInventory("")
	}

	if err != nil {
		return err
	}

	if err := graph.CheckForRequiredInputs(inventory, cfg); err != nil {
		return fmt.Errorf("failed to check inventory for required inputs: %w", err)
	}

	hostContexts, err := GetHostContexts(inventory, graph, cfg)
	if err != nil {
		return err
	}
	defer func() {
		for _, hc := range hostContexts {
			if closeErr := hc.Close(); closeErr != nil {
				common.LogWarn("Failed to close host context", map[string]interface{}{
					"host":  hc.Host.Name,
					"error": closeErr.Error(),
				})
			}
		}
	}()

	// Use logging instead of fmt.Printf to avoid deadlocks with Temporal's error handling
	if cfg.Logging.Format == "plain" {
		common.LogInfo("PLAY []", map[string]interface{}{})
	} else {
		common.LogInfo("Starting play", map[string]interface{}{})
	}
	orderedGraph, err := GetOrderedGraph(cfg, graph)
	if err != nil {
		return err
	}

	currCwd := ChangeCWDToPlaybookDir(graph.PlaybookPath)
	defer func() {
		if err := os.Chdir(currCwd); err != nil {
			log.Fatalf("Failed to change directory back to %s: %v", currCwd, err)
		}
	}()

	// Create daemon client if provided
	var daemonClient *daemon.Client
	if daemonClientInterface != nil {
		if client, ok := daemonClientInterface.(*daemon.Client); ok {
			daemonClient = client
		}
	}

	// Store daemon client in a context that can be accessed by executors
	// We'll use a global variable or pass it through the config for now
	if daemonClient != nil {
		// For now, we'll store it in the config as a temporary solution
		// This is not ideal but avoids breaking the interface
		cfg.SetDaemonReporting(daemonClient)
		if err := ReportPlayStart(daemonClient, graph.PlaybookPath, inventoryFile, cfg.Executor); err != nil {
			common.LogWarn("failed to report play start", map[string]interface{}{"error": err.Error()})
		}
	}

	err = executor.Execute(hostContexts, orderedGraph, cfg)
	if err != nil {
		if err := ReportPlayError(daemonClient, err); err != nil {
			common.LogWarn("failed to report play error", map[string]interface{}{"error": err.Error()})
		}
	} else {
		if err := ReportPlayCompletion(daemonClient); err != nil {
			common.LogWarn("failed to report play completion", map[string]interface{}{"error": err.Error()})
		}
	}
	return err
}

// Execute implements the main execution loop for a Spage graph.
func GetHostContexts(inventory *Inventory, graph *Graph, cfg *config.Config) (map[string]*HostContext, error) {
	contexts, err := GetContextForRun(inventory, graph, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get host contexts for run: %w", err)
	}
	return contexts, nil
}

func GetOrderedGraph(cfg *config.Config, graph *Graph) ([][]Task, error) {
	switch cfg.ExecutionMode {
	case "parallel":
		return graph.ParallelTasks(), nil
	case "sequential":
		return graph.SequentialTasks(), nil
	default:
		return nil, fmt.Errorf("unknown or unsupported execution mode: %s", cfg.ExecutionMode)
	}
}
