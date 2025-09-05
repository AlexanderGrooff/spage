package modules

import (
	"fmt"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg"
	"gopkg.in/yaml.v3"
)

type BlockModule struct{}

func (m BlockModule) InputType() reflect.Type {
	return reflect.TypeOf(BlockInput{})
}

func (m BlockModule) OutputType() reflect.Type {
	return reflect.TypeOf(BlockOutput{})
}

type BlockInput struct {
}

func (i BlockInput) GetVariableUsage() []string {
	return []string{}
}

func (i BlockInput) ProvidesVariables() []string {
	return []string{}
}

func (i BlockInput) HasRevert() bool {
	return false
}

func (i BlockInput) ToCode() string {
	return "modules.BlockInput{}"
}

func (i BlockInput) Validate() error {
	return nil
}

type BlockOutput struct {
	pkg.ModuleOutput
}

func (b BlockOutput) String() string {
	return "Block executed"
}

func (m BlockModule) EvaluateExecute(metatask *pkg.MetaTask, closure *pkg.Closure) chan pkg.TaskResult {
	// Create a single result channel for the block
	results := make(chan pkg.TaskResult, 1)

	go func() {
		defer close(results)
		var allResults []pkg.TaskResult
		var blockFailed bool

		// Execute children tasks
		for _, task := range metatask.Children {
			taskResultChan := task.ExecuteModule(closure)
			taskResult := <-taskResultChan
			allResults = append(allResults, taskResult)
			if taskResult.Status == pkg.TaskStatusFailed {
				blockFailed = true
			}
		}

		// If block failed, execute rescue tasks
		if blockFailed {
			rescueFailed := false
			for _, task := range metatask.Rescue {
				taskResultChan := task.ExecuteModule(closure)
				taskResult := <-taskResultChan
				allResults = append(allResults, taskResult)
				if taskResult.Status == pkg.TaskStatusFailed {
					rescueFailed = true
				}
			}
			// If rescue succeeded, the block is recovered
			if !rescueFailed {
				blockFailed = false
			}
		}

		// Always execute always tasks
		for _, task := range metatask.Always {
			taskResultChan := task.ExecuteModule(closure)
			taskResult := <-taskResultChan
			allResults = append(allResults, taskResult)
		}

		// Determine the overall block status
		var finalStatus pkg.TaskStatus
		var finalChanged bool
		var finalError error

		if blockFailed {
			finalStatus = pkg.TaskStatusFailed
			finalError = fmt.Errorf("block failed")
		} else {
			finalStatus = pkg.TaskStatusOk
			// Check if any task changed something
			for _, result := range allResults {
				if result.Changed {
					finalChanged = true
					break
				}
			}
		}

		// Create the final block result
		finalResult := pkg.TaskResult{
			Task:    metatask,
			Closure: closure,
			Status:  finalStatus,
			Failed:  finalStatus == pkg.TaskStatusFailed,
			Changed: finalChanged,
			Error:   finalError,
			Output:  BlockOutput{},
		}

		// Send the single block result
		results <- finalResult
	}()

	return results
}

func (m BlockModule) EvaluateRevert(metatask *pkg.MetaTask, closure *pkg.Closure) chan pkg.TaskResult {
	// Calculate total number of tasks that might produce results
	totalTasks := len(metatask.Children)
	// Always tasks always run, so add them to the count
	totalTasks += len(metatask.Always)
	// Rescue tasks only run if the block failed, so we don't count them here

	// Create buffered channel with enough capacity for all tasks
	results := make(chan pkg.TaskResult, totalTasks)

	// Execute all tasks in the block and collect results first
	var allResults []pkg.TaskResult

	// Revert children tasks (in reverse order)
	for i := len(metatask.Children) - 1; i >= 0; i-- {
		task := metatask.Children[i]
		// Execute the actual task
		taskResultChan := task.RevertModule(closure)
		// Wait for exactly one result (like regular tasks do)
		taskResult := <-taskResultChan

		allResults = append(allResults, taskResult)
	}

	// Always revert always tasks (in reverse order)
	for i := len(metatask.Always) - 1; i >= 0; i-- {
		task := metatask.Always[i]
		// Execute the always task
		taskResultChan := task.RevertModule(closure)
		// Wait for exactly one result
		taskResult := <-taskResultChan

		allResults = append(allResults, taskResult)
	}

	// Send all results at once
	for _, result := range allResults {
		results <- result
	}

	// Close the channel to signal completion
	close(results)

	return results
}

func (m BlockModule) ParameterAliases() map[string]string {
	return nil
}

// UnmarshalYAML unpacks the block input into the BlockInput struct.
// This is necessary to store the list of nodes into Children.
func (i BlockInput) UnmarshalYAML(node *yaml.Node) error {
	// This is handled by TextToGraphNodes

	// i.Tasks = []pkg.GraphNode{}

	// if node.Kind == yaml.SequenceNode {
	// 	if err := node.Decode(&i.Tasks); err != nil {
	// 		return fmt.Errorf("failed to decode block input tasks: %w", err)
	// 	}
	// }
	return nil
}

func init() {
	pkg.RegisterMetaModule("block", BlockModule{})
}
