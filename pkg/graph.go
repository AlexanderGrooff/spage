package pkg

import (
	"fmt"
	"strings"
)

// Don't look for dependencies for these vars
var SpecialVars = []string{
	"previous", // Provided in 'revert' context
}

type Graph struct {
	Sequential bool
	Tasks      [][]Task
}

func NewGraph(tasks []Task) (Graph, error) {
	g := Graph{}
	dependsOn := map[string][]string{}
	taskNameMapping := map[string]Task{}
	dependsOnVariables := map[string][]string{}
	variableProvidedBy := map[string]string{}
	for _, task := range tasks {
		taskNameMapping[task.Name] = task
		fmt.Printf("Adding task %s to key %s: %s\n", task, task.Name, taskNameMapping[task.Name])
		if task.Before != "" {
			dependsOn[task.Before] = append(dependsOn[task.Before], task.Name)
		}
		if task.After != "" {
			dependsOn[task.Name] = append(dependsOn[task.Name], task.After)
		}
		// TODO: catch cyclic graph
		if task.Register != "" {
			variableProvidedBy[strings.ToLower(task.Register)] = task.Name
		}
		// Note: task cannot use its own variable
		dependsOnVariables[task.Name] = task.Params.GetVariableUsage()
	}
	// TODO: loop over variable dependencies
	for taskName, vars := range dependsOnVariables {
		for _, varName := range vars {
			providingTask, ok := variableProvidedBy[varName]
			if !ok {
				if !containsInSlice(SpecialVars, varName) {
					return Graph{}, fmt.Errorf("no task found that provides variable %q for task %q", varName, taskName)
				}
			} else {
				DebugOutput("Found that task %q depends on %q for variable %q", taskName, providingTask, varName)
				dependsOn[taskName] = append(dependsOn[taskName], providingTask)
			}
		}
	}

	executedOnStep := map[string]int{}
	for taskName, _ := range taskNameMapping {
		executedOnStep = ResolveExecutionLevel(taskName, dependsOn, executedOnStep)
		if len(executedOnStep) == len(taskNameMapping) {
			break
		}
	}
	var maxExecutionLevel int
	for _, executionLevel := range executedOnStep {
		maxExecutionLevel = max(maxExecutionLevel, executionLevel)
	}

	g.Tasks = make([][]Task, maxExecutionLevel+1)
	for taskName, executionLevel := range executedOnStep {
		task := taskNameMapping[taskName]
		fmt.Printf("%s on execution level %d\n", task, executionLevel)
		g.Tasks[executionLevel] = append(g.Tasks[executionLevel], task)
	}
	fmt.Printf("Found dependencies: %s\n", dependsOn)
	fmt.Printf("Found tasks: %s\n", taskNameMapping)
	fmt.Printf("Found execution levels: %s\n", executedOnStep)

	return g, nil
}

func ResolveExecutionLevel(taskName string, dependsOn map[string][]string, executedOnStep map[string]int) map[string]int {
	if len(dependsOn[taskName]) == 0 {
		executedOnStep[taskName] = 0
		return executedOnStep
	}
	for _, parentTaskName := range dependsOn[taskName] {
		executedOnStep = ResolveExecutionLevel(parentTaskName, dependsOn, executedOnStep)
		executedOnStep[taskName] = max(executedOnStep[taskName], executedOnStep[parentTaskName]+1)
	}
	return executedOnStep
}
