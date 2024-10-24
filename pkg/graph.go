package pkg

import "fmt"

type Graph struct {
	Sequential bool
	Tasks      [][]Task
}

func NewGraph(tasks []Task) Graph {
	g := Graph{}
	dependsOn := map[string][]string{}
	taskNameMapping := map[string]Task{}
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
		// TODO: set dependencies based on registered and used vars
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

	return g
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
