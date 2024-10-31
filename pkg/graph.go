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

func (g Graph) String() string {
	var b strings.Builder
	for i, tasks := range g.Tasks {
		fmt.Fprintf(&b, "- Step %d:\n", i)
		for _, task := range tasks {
			fmt.Fprintf(&b, "---- %s\n", task.Name)
		}
	}
	return b.String()
}

func NewGraph(tasks []Task) (Graph, error) {
	g := Graph{}
	dependsOn := map[string][]string{}
	taskNameMapping := map[string]Task{}
	dependsOnVariables := map[string][]string{}
	variableProvidedBy := map[string]string{}
	visited := map[string]bool{}
	recStack := map[string]bool{}

	for _, task := range tasks {
		if err := task.Params.Validate(); err != nil {
			return Graph{}, fmt.Errorf("task %q failed to validate: %s", task.Name, err)
		}
		taskNameMapping[task.Name] = task
		if task.Before != "" {
			dependsOn[task.Before] = append(dependsOn[task.Before], task.Name)
		}
		if task.After != "" {
			dependsOn[task.Name] = append(dependsOn[task.Name], task.After)
		}
		if task.Register != "" {
			variableProvidedBy[strings.ToLower(task.Register)] = task.Name
		}
		// Note: task cannot use its own variable
		dependsOnVariables[task.Name] = task.Params.GetVariableUsage()
	}

	// Check for cycles
	for taskName := range taskNameMapping {
		if err := checkCycle(taskName, dependsOn, visited, recStack); err != nil {
			return Graph{}, err
		}
	}

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
		g.Tasks[executionLevel] = append(g.Tasks[executionLevel], task)
	}

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

func checkCycle(taskName string, dependsOn map[string][]string, visited, recStack map[string]bool) error {
	if !visited[taskName] {
		visited[taskName] = true
		recStack[taskName] = true

		for _, parentTaskName := range dependsOn[taskName] {
			if !visited[parentTaskName] {
				if err := checkCycle(parentTaskName, dependsOn, visited, recStack); err != nil {
					return err
				}
			} else if recStack[parentTaskName] {
				return fmt.Errorf("cyclic dependency detected involving task %q", taskName)
			}
		}
	}
	recStack[taskName] = false
	return nil
}
