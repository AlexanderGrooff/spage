package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/compile"
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
	common.LogInfo("Compiling graph to code", map[string]interface{}{
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

// Order tasks by their id
func (g Graph) SequentialTasks() [][]GraphNode {
	maxId := -1
	for _, nodes := range g.Tasks {
		for _, node := range nodes {
			if task, ok := node.(Task); ok {
				if task.Id > maxId {
					maxId = task.Id
				}
			}
		}
	}

	var sortedTasks [][]GraphNode = make([][]GraphNode, maxId+1)
	for _, nodes := range g.Tasks {
		for _, node := range nodes {
			sortedTasks[node.(Task).Id] = []GraphNode{node}
		}
	}
	return sortedTasks
}

// Order tasks based on execution level
func (g Graph) ParallelTasks() [][]GraphNode {
	return g.Tasks
}

func NewGraphFromFile(path string) (Graph, error) {
	// Read YAML file
	data, err := os.ReadFile(path)
	if err != nil {
		return Graph{}, fmt.Errorf("error reading YAML file %s: %v", path, err)
	}
	// Determine base path for resolving relative includes/roles
	basePath := filepath.Dir(path)
	// Preprocess the playbook to handle plays, includes, roles
	processedNodes, err := compile.PreprocessPlaybook(data, basePath)
	if err != nil {
		return Graph{}, fmt.Errorf("error preprocessing playbook %s: %w", path, err)
	}

	// Parse the preprocessed nodes into tasks
	tasks, err := TextToGraphNodes(processedNodes)
	if err != nil {
		return Graph{}, fmt.Errorf("error parsing preprocessed tasks from %s: %w", path, err)
	}

	graph, err := NewGraph(tasks)
	if err != nil {
		return Graph{}, fmt.Errorf("failed to generate graph from %s: %w", path, err)
	}
	return graph, nil
}

func NewGraphFromPlaybook(data []byte) (Graph, error) {
	// Preprocess the playbook data with current directory as base path
	basePath := "."
	processedNodes, err := compile.PreprocessPlaybook(data, basePath)
	if err != nil {
		return Graph{}, fmt.Errorf("error preprocessing playbook data: %w", err)
	}

	// Parse YAML nodes into tasks
	tasks, err := TextToGraphNodes(processedNodes)
	if err != nil {
		return Graph{}, fmt.Errorf("error parsing preprocessed tasks: %w", err)
	}

	graph, err := NewGraph(tasks)
	if err != nil {
		return Graph{}, fmt.Errorf("failed to generate graph: %w", err)
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

// Helper function to flatten GraphNodes into a list of TaskNodes
// while preserving a semblance of original order.
func flattenNodes(nodes []GraphNode) []TaskNode {
	var flatTasks []TaskNode
	var collectTasks func(node GraphNode)
	collectTasks = func(node GraphNode) {
		switch n := node.(type) {
		case TaskNode:
			flatTasks = append(flatTasks, n)
		case Task:
			// Convert Task to TaskNode before adding
			flatTasks = append(flatTasks, TaskNode{Task: n})
		case Graph:
			// Recursively process nodes within the nested graph's internal structure first.
			for _, step := range n.Tasks {
				for _, subNode := range step {
					collectTasks(subNode)
				}
			}
			// default: // Should not happen if parsing is correct, ignore unknown types
		}
	}

	for _, node := range nodes {
		collectTasks(node)
	}
	return flatTasks
}

func GetVariableUsage(task TaskNode) []string {
	varsUsage := task.Params.GetVariableUsage()
	if task.Loop != nil {
		// TODO: change name of the variable if loopcontrol is used
		varsUsage = common.RemoveFromSlice(varsUsage, "item")
		if loop, ok := task.Loop.(string); ok {
			varsUsage = append(varsUsage, GetVariableUsageFromTemplate(loop)...)
		}
	}
	return varsUsage
}

func NewGraph(nodes []GraphNode) (Graph, error) {
	common.LogDebug("NewGraph received nodes.", map[string]interface{}{"count": len(nodes)}) // Log input count
	g := Graph{RequiredInputs: []string{}}
	dependsOn := map[string][]string{}
	taskNameMapping := map[string]TaskNode{}
	originalIndexMap := map[string]int{} // Map task name to its original flattened index
	dependsOnVariables := map[string][]string{}
	variableProvidedBy := map[string]string{}
	visited := map[string]bool{}
	recStack := map[string]bool{}

	// 1. Flatten nodes and record original order index
	flattenedTasks := flattenNodes(nodes)
	for i, taskNode := range flattenedTasks {
		if _, exists := taskNameMapping[taskNode.Name]; exists {
			// Handle potential duplicate task names if necessary, though Ansible usually requires unique names within a play
			common.LogWarn("Duplicate task name found during flattening", map[string]interface{}{"name": taskNode.Name})
			// For now, we'll overwrite, assuming later tasks with the same name take precedence or are errors
		}
		taskNameMapping[taskNode.Name] = taskNode
		originalIndexMap[taskNode.Name] = i
	}

	// 2. Build dependencies based on flattened tasks
	for _, n := range taskNameMapping {
		common.DebugOutput("Processing node TaskNode %q %q: %+v", n.Name, n.Module, n.Params)
		if n.Params == nil {
			common.DebugOutput("Task %q has no params, skipping dependency analysis for params", n.Name)
			// Continue processing other dependencies like before/after
		} else {
			dependsOnVariables[n.Name] = GetVariableUsage(n)
		}

		if n.Before != "" {
			// Task 'n' must run *before* task 'n.Before'
			// So, 'n.Before' depends on 'n'
			dependsOn[n.Before] = append(dependsOn[n.Before], n.Name)
		}
		if n.After != "" {
			// Task 'n' must run *after* task 'n.After'
			// So, 'n' depends on 'n.After'
			dependsOn[n.Name] = append(dependsOn[n.Name], n.After)
		}
		if n.Register != "" {
			variableProvidedBy[strings.ToLower(n.Register)] = n.Name
		}

		// Check if the module's parameters inherently provide variables (like set_fact)
		if n.Params != nil {
			providedVars := n.Params.ProvidesVariables()
			for _, providedVar := range providedVars {
				// TODO: Consider case sensitivity/normalization if needed (Ansible usually lowercases)
				variableProvidedBy[providedVar] = n.Name
			}
		}
	}

	// Extract required inputs from nested graphs (if any were present in the original structure)
	var collectRequiredInputs func(node GraphNode)
	collectRequiredInputs = func(node GraphNode) {
		switch n := node.(type) {
		case Graph:
			for _, input := range n.RequiredInputs {
				if !containsInSlice(g.RequiredInputs, input) {
					g.RequiredInputs = append(g.RequiredInputs, input)
				}
			}
			// Recursively check nested graphs
			for _, step := range n.Tasks {
				for _, subNode := range step {
					collectRequiredInputs(subNode)
				}
			}
			// Ignore TaskNode and Task types for required inputs collection
		}
	}
	for _, node := range nodes { // Iterate original nodes structure for nested graph inputs
		collectRequiredInputs(node)
	}

	// 3. Check for cycles
	for taskName := range taskNameMapping {
		if err := checkCycle(taskName, dependsOn, visited, recStack); err != nil {
			return Graph{}, err
		}
	}

	// 4. Process variable dependencies
	for taskName, vars := range dependsOnVariables {
		for _, varName := range vars {
			// Skip special vars like 'previous'
			if containsInSlice(SpecialVars, varName) {
				continue
			}

			providingTask, ok := variableProvidedBy[varName]
			if !ok {
				common.DebugOutput("no task found that provides variable %q for task %q", varName, taskName)
				if !containsInSlice(g.RequiredInputs, varName) {
					g.RequiredInputs = append(g.RequiredInputs, varName)
				}

			} else {
				// Ensure the dependency is only added if the tasks are different
				// and the dependency doesn't already exist to avoid duplicates.
				if taskName != providingTask && !containsInSlice(dependsOn[taskName], providingTask) {
					common.DebugOutput("Found that task %q depends on %q for variable %q", taskName, providingTask, varName)
					dependsOn[taskName] = append(dependsOn[taskName], providingTask)
				}
			}
		}
	}

	// 5. Resolve execution levels
	executedOnStep := map[string]int{}
	allTaskNames := make([]string, 0, len(taskNameMapping))
	for taskName := range taskNameMapping {
		allTaskNames = append(allTaskNames, taskName)
	}
	// Sort task names to ensure deterministic processing order for ResolveExecutionLevel
	// This helps make the *level assignment* itself more stable, although the final sort step is the primary guarantee.
	sort.Strings(allTaskNames)
	for _, taskName := range allTaskNames {
		if _, processed := executedOnStep[taskName]; !processed {
			executedOnStep = ResolveExecutionLevel(taskName, dependsOn, executedOnStep)
		}
	}

	// 6. Determine max execution level and initialize task slices
	var maxExecutionLevel int
	for _, executionLevel := range executedOnStep {
		maxExecutionLevel = max(maxExecutionLevel, executionLevel)
	}

	g.Tasks = make([][]GraphNode, maxExecutionLevel+1)
	for i := range g.Tasks {
		g.Tasks[i] = []GraphNode{}
	}

	// 7. Populate tasks into levels
	for taskName, executionLevel := range executedOnStep {
		task := taskNameMapping[taskName]
		g.Tasks[executionLevel] = append(g.Tasks[executionLevel], task)
	}

	// 8. Sort tasks within each level based on original index
	for i := range g.Tasks {
		sort.SliceStable(g.Tasks[i], func(a, b int) bool {
			taskA, okA := g.Tasks[i][a].(TaskNode)
			taskB, okB := g.Tasks[i][b].(TaskNode)
			if !okA || !okB {
				// Should not happen if only TaskNodes are added
				// If other types could be added, handle comparison logic here
				return false // Or some default order
			}
			// Compare based on the original flattened index
			return originalIndexMap[taskA.Name] < originalIndexMap[taskB.Name]
		})
	}

	common.LogDebug("NewGraph finished building.", map[string]interface{}{"graph": g.String()}) // Log final graph string
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
	common.DebugOutput("Checking inventory for required inputs %+v", inventory)
	for _, host := range inventory.Hosts {
		for _, input := range g.RequiredInputs {
			common.DebugOutput("Checking if required input %q is present in inventory for host %q", input, host.Name)
			if _, ok := host.Vars[input]; !ok {
				return fmt.Errorf("required input %q not found in inventory for host %q", input, host.Name)
			}
		}
	}
	return nil
}
