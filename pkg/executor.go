package pkg

import (
	"context"
	"fmt"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
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

func ExecuteGraph(executor GraphExecutor, graph Graph, inventoryFile string, cfg *config.Config) error {
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

	if err := graph.CheckInventoryForRequiredInputs(inventory); err != nil {
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

	if cfg.Logging.Format == "plain" {
		fmt.Printf("\nPLAY [] ****************************************************\n")
	} else {
		common.LogInfo("Starting play", map[string]interface{}{})
	}
	orderedGraph, err := GetOrderedGraph(cfg, graph)
	if err != nil {
		return err
	}

	err = executor.Execute(hostContexts, orderedGraph, cfg)
	return err
}

// Execute implements the main execution loop for a Spage graph.
func GetHostContexts(inventory *Inventory, graph Graph, cfg *config.Config) (map[string]*HostContext, error) {
	contexts, err := GetContextForRun(inventory, graph, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get host contexts for run: %w", err)
	}
	return contexts, nil
}

func GetOrderedGraph(cfg *config.Config, graph Graph) ([][]Task, error) {
	switch cfg.ExecutionMode {
	case "parallel":
		return graph.ParallelTasks(), nil
	case "sequential":
		return graph.SequentialTasks(), nil
	default:
		return nil, fmt.Errorf("unknown or unsupported execution mode: %s", cfg.ExecutionMode)
	}
}
