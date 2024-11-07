package pkg

import (
	"fmt"
	"os"
	"strings"
)

// Don't look for dependencies for these vars
var SpecialVars = []string{
	"previous", // Provided in 'revert' context
}

type Graph struct {
	RequiredInputs []string
	Tasks          [][]Task
}

func (g Graph) String() string {
	var b strings.Builder
	for i, tasks := range g.Tasks {
		fmt.Fprintf(&b, "- Step %d:\n", i)
		for _, task := range tasks {
			fmt.Fprintf(&b, "---- %s\n", task.Name)
		}
	}
	fmt.Fprintf(&b, "Required inputs:\n")
	for _, input := range g.RequiredInputs {
		fmt.Fprintf(&b, "  - %s\n", input)
	}
	return b.String()
}

func (g Graph) ToCode() string {
	var f strings.Builder
	fmt.Fprintln(&f, "var Graph = pkg.Graph{")
	fmt.Fprintf(&f, "%sRequiredInputs: []string{\n", Indent(1))
	for _, input := range g.RequiredInputs {
		fmt.Fprintf(&f, "%s  %q,\n", Indent(2), input)
	}
	fmt.Fprintf(&f, "%s},\n", Indent(1))
	fmt.Fprintf(&f, "%sTasks: [][]pkg.Task{\n", Indent(1))

	for _, taskExecutionLevel := range g.Tasks {
		fmt.Fprintf(&f, "%s[]pkg.Task{\n", Indent(2))
		for _, task := range taskExecutionLevel {
			fmt.Fprintf(&f, "%s", task.ToCode(3))
		}
		fmt.Fprintf(&f, "%s},\n", Indent(2))
	}

	fmt.Fprintf(&f, "%s},\n", Indent(1))
	fmt.Fprintln(&f, "}")
	return f.String()
}

func NewGraphFromFile(path string) (Graph, error) {
	// Read YAML file
	data, err := os.ReadFile(path)
	if err != nil {
		return Graph{}, fmt.Errorf("error reading YAML file: %v", err)
	}

	// Parse YAML
	tasks, err := TextToTasks(data)
	if err != nil {
		return Graph{}, fmt.Errorf("error parsing YAML: %v", err)
	}

	graph, err := NewGraph(tasks)
	if err != nil {
		return Graph{}, fmt.Errorf("failed to generate graph: %s", err)
	}
	return graph, nil
}

func NewGraph(tasks []Task) (Graph, error) {
	g := Graph{RequiredInputs: []string{}}
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
					DebugOutput("no task found that provides variable %q for task %q", varName, taskName)
					g.RequiredInputs = append(g.RequiredInputs, varName)
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

func (g Graph) CheckInventoryForRequiredInputs(inventory *Inventory) error {
	DebugOutput("Checking inventory for required inputs %+v", inventory)
	for _, host := range inventory.Hosts {
		for _, input := range g.RequiredInputs {
			DebugOutput("Checking if required input %q is present in inventory for host %q", input, host.Name)
			if _, ok := host.Vars[input]; !ok {
				return fmt.Errorf("required input %q not found in inventory for host %q", input, host.Name)
			}
		}
	}
	return nil
}
