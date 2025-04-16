package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Don't look for dependencies for these vars
var SpecialVars = []string{
	"previous", // Provided in 'revert' context
}

// GraphNode represents either a list of tasks or a nested graph
type GraphNode interface {
	String() string
	ToCode() string
}

type Graph struct {
	RequiredInputs []string
	Tasks          [][]GraphNode
}

func (g Graph) String() string {
	var b strings.Builder
	for i, node := range g.Tasks {
		fmt.Fprintf(&b, "- Step %d:\n", i)
		for _, task := range node {
			fmt.Fprintf(&b, "  - %s\n", task.String())
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
	fmt.Fprintln(&f, "var GeneratedGraph = pkg.Graph{")
	fmt.Fprintf(&f, "%sRequiredInputs: []string{\n", Indent(1))
	for _, input := range g.RequiredInputs {
		fmt.Fprintf(&f, "%s  %q,\n", Indent(2), input)
	}
	fmt.Fprintf(&f, "%s},\n", Indent(1))
	fmt.Fprintf(&f, "%sTasks: [][]pkg.GraphNode{\n", Indent(1))

	for _, node := range g.Tasks {
		fmt.Fprintf(&f, "%s  []pkg.GraphNode{\n", Indent(2))
		for _, task := range node {
			fmt.Fprintf(&f, "%s    %s", Indent(3), task.ToCode())
		}
		fmt.Fprintf(&f, "%s  },\n", Indent(2))
	}

	fmt.Fprintf(&f, "%s},\n", Indent(1))
	fmt.Fprintln(&f, "}")
	return f.String()
}

func (g Graph) SaveToFile(path string) error {
	// Create all parent directories if they don't exist
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("error creating directories: %v", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("error creating file: %v", err)
	}
	defer f.Close()
	LogInfo("Compiling graph to code", map[string]interface{}{
		"graph": g.String(),
	})

	fmt.Fprintln(f, "package main")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "import (")
	fmt.Fprintln(f, `    "os"`)
	fmt.Fprintln(f, `    "fmt"`)
	fmt.Fprintln(f, `    "flag"`)
	fmt.Fprintln(f, `    "github.com/AlexanderGrooff/spage/cmd"`)
	fmt.Fprintln(f, `    "github.com/AlexanderGrooff/spage/pkg"`)
	fmt.Fprintln(f, `    "github.com/AlexanderGrooff/spage/pkg/modules"`)
	fmt.Fprintln(f, ")")
	fmt.Fprintln(f)
	fmt.Fprint(f, g.ToCode())
	fmt.Fprintln(f)
	fmt.Fprintln(f, "func main() {")
	fmt.Fprintln(f, "    configFile := flag.String(\"config\", \"\", \"Config file path (default: ./spage.yaml)\")")
	fmt.Fprintln(f, "    inventoryFile := flag.String(\"inventory\", \"\", \"Inventory file path\")")
	fmt.Fprintln(f, "    flag.Parse()")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "    // Load configuration and apply logging settings")
	fmt.Fprintln(f, "    err := cmd.LoadConfig(*configFile)")
	fmt.Fprintln(f, "    if err != nil {")
	fmt.Fprintln(f, "        fmt.Printf(\"Error loading config: %v\\n\", err)")
	fmt.Fprintln(f, "        os.Exit(1)")
	fmt.Fprintln(f, "    }")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "    // Execute the graph using the loaded configuration")
	fmt.Fprintln(f, "    cfg := cmd.GetConfig() // Function to get the loaded config from cmd package")
	fmt.Fprintln(f, "    err = pkg.Execute(cfg, GeneratedGraph, *inventoryFile)")
	fmt.Fprintln(f, "    if err != nil {")
	fmt.Fprintln(f, "        fmt.Printf(\"Execution failed: %v\\n\", err)")
	fmt.Fprintln(f, "        os.Exit(1)")
	fmt.Fprintln(f, "    }")
	fmt.Fprintln(f, "}")
	return nil
}

func NewGraphFromFile(path string) (Graph, error) {
	// Read YAML file
	data, err := os.ReadFile(path)
	if err != nil {
		return Graph{}, fmt.Errorf("error reading YAML file: %v", err)
	}
	return NewGraphFromPlaybook(data)
}

func NewGraphFromPlaybook(data []byte) (Graph, error) {

	// Parse YAML
	tasks, err := TextToGraphNodes(data)
	if err != nil {
		return Graph{}, fmt.Errorf("error parsing YAML: %v", err)
	}

	graph, err := NewGraph(tasks)
	if err != nil {
		return Graph{}, fmt.Errorf("failed to generate graph: %s", err)
	}
	return graph, nil
}

// TaskNode represents a single task in the graph
type TaskNode struct {
	Task
}

func (t TaskNode) String() string {
	return t.Task.String()
}

func (t TaskNode) ToCode() string {
	return t.Task.ToCode()
}

func NewGraph(nodes []GraphNode) (Graph, error) {
	g := Graph{RequiredInputs: []string{}}
	dependsOn := map[string][]string{}
	taskNameMapping := map[string]TaskNode{}
	dependsOnVariables := map[string][]string{}
	variableProvidedBy := map[string]string{}
	visited := map[string]bool{}
	recStack := map[string]bool{}

	// First, flatten all nested graphs and collect tasks
	var processTasks func(node GraphNode) error
	processTasks = func(node GraphNode) error {
		switch n := node.(type) {
		case TaskNode:
			DebugOutput("Processing node %T %q %q: %+v", node, n.Name, n.Module, n.Params)
			if n.Params == nil {
				DebugOutput("Task %q has no params, skipping", n.Name)
				return nil
			}
			taskNameMapping[n.Name] = n
			if n.Before != "" {
				dependsOn[n.Before] = append(dependsOn[n.Before], n.Name)
			}
			if n.After != "" {
				dependsOn[n.Name] = append(dependsOn[n.Name], n.After)
			}
			if n.Register != "" {
				variableProvidedBy[strings.ToLower(n.Register)] = n.Name
			}
			dependsOnVariables[n.Name] = n.Params.GetVariableUsage()
		case Graph:
			DebugOutput("Processing nested graph %q", n)
			for _, subNode := range n.Tasks {
				for _, node := range subNode {
					if err := processTasks(node); err != nil {
						return err
					}
				}
			}
			for _, input := range n.RequiredInputs {
				if !containsInSlice(g.RequiredInputs, input) {
					g.RequiredInputs = append(g.RequiredInputs, input)
				}
			}
		case Task:
			// Convert Task to TaskNode and process it
			taskNode := TaskNode{Task: n}
			return processTasks(taskNode)
		default:
			return fmt.Errorf("unknown node type: %T", node)
		}
		return nil
	}

	// Process all nodes
	for _, node := range nodes {
		if err := processTasks(node); err != nil {
			return Graph{}, err
		}
	}

	// Check for cycles
	for taskName := range taskNameMapping {
		if err := checkCycle(taskName, dependsOn, visited, recStack); err != nil {
			return Graph{}, err
		}
	}

	// Process variable dependencies
	for taskName, vars := range dependsOnVariables {
		for _, varName := range vars {
			providingTask, ok := variableProvidedBy[varName]
			if !ok {
				if !containsInSlice(SpecialVars, varName) {
					DebugOutput("no task found that provides variable %q for task %q", varName, taskName)
					if !containsInSlice(g.RequiredInputs, varName) {
						g.RequiredInputs = append(g.RequiredInputs, varName)
					}
				}
			} else {
				DebugOutput("Found that task %q depends on %q for variable %q", taskName, providingTask, varName)
				dependsOn[taskName] = append(dependsOn[taskName], providingTask)
			}
		}
	}

	executedOnStep := map[string]int{}
	for taskName := range taskNameMapping {
		executedOnStep = ResolveExecutionLevel(taskName, dependsOn, executedOnStep)
		if len(executedOnStep) == len(taskNameMapping) {
			break
		}
	}
	var maxExecutionLevel int
	for _, executionLevel := range executedOnStep {
		maxExecutionLevel = max(maxExecutionLevel, executionLevel)
	}

	g.Tasks = make([][]GraphNode, maxExecutionLevel+1)
	for i := range g.Tasks {
		g.Tasks[i] = []GraphNode{}
	}

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
