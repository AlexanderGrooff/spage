package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/compile"
)

// Don't look for dependencies for these vars
var SpecialVars = []string{
	"previous", // Provided in 'revert' context
}

// Facts that can be gathered by the setup module
var AllowedFacts = map[string]struct{}{
	"platform":           {},
	"user":               {},
	"inventory_hostname": {},
	"ssh_host_pub_keys":  {},
}

// GraphNode represents either a list of tasks or a nested graph
type GraphNode interface {
	String() string
	ToCode() string
}

type Graph struct {
	RequiredInputs []string
	Tasks          [][]Task
	Handlers       []Task
	Vars           map[string]interface{}
}

func (g Graph) String() string {
	var b strings.Builder
	for i, node := range g.Tasks {
		fmt.Fprintf(&b, "- Step %d:\n", i)
		for _, task := range node {
			fmt.Fprintf(&b, "  - %s\n", task.String())
		}
	}
	fmt.Fprintf(&b, "Handlers:\n")
	for _, handler := range g.Handlers {
		fmt.Fprintf(&b, "  - %s\n", handler.String())
	}
	fmt.Fprintf(&b, "Required inputs:\n")
	for _, input := range g.RequiredInputs {
		fmt.Fprintf(&b, "  - %s\n", input)
	}
	fmt.Fprintf(&b, "Vars:\n")
	for k, v := range g.Vars {
		fmt.Fprintf(&b, "  - %s: %#v\n", k, v)
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
	fmt.Fprintf(&f, "%sVars: map[string]interface{}{\n", Indent(1))
	for k, v := range g.Vars {
		fmt.Fprintf(&f, "%s  %q: %#v,\n", Indent(2), k, v)
	}
	fmt.Fprintf(&f, "%s},\n", Indent(1))
	fmt.Fprintf(&f, "%sTasks: [][]pkg.Task{\n", Indent(1))

	for _, node := range g.Tasks {
		fmt.Fprintf(&f, "%s  []pkg.Task{\n", Indent(2))
		for _, task := range node {
			fmt.Fprintf(&f, "%s    %s", Indent(3), task.ToCode())
		}
		fmt.Fprintf(&f, "%s  },\n", Indent(2))
	}

	fmt.Fprintf(&f, "%s},\n", Indent(1))
	fmt.Fprintf(&f, "%sHandlers: []pkg.Task{\n", Indent(1))
	for _, handler := range g.Handlers {
		fmt.Fprintf(&f, "%s  %s", Indent(2), handler.ToCode())
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
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			common.LogWarn("Failed to close file", map[string]interface{}{
				"file":  path,
				"error": closeErr.Error(),
			})
		}
	}()
	common.LogInfo("Compiling graph to code", map[string]interface{}{
		"graph": g.String(),
	})

	graphCode := g.ToCode()

	var content strings.Builder
	content.WriteString(`package main

import (
    "os"
    "github.com/AlexanderGrooff/spage/cmd"
    "github.com/AlexanderGrooff/spage/pkg"
    "github.com/AlexanderGrooff/spage/pkg/common"
    "github.com/AlexanderGrooff/spage/pkg/modules"
)

`)

	// Inject the GeneratedGraph definition
	content.WriteString(graphCode)
	content.WriteString("\n")
	content.WriteString(`func main() {
    err := cmd.EntrypointLocalExecutor(GeneratedGraph)
    if err != nil {
        common.LogError("Failed to run playbook", map[string]interface{}{
            "error": err.Error(),
        })
        os.Exit(1)
    }
}
`)

	_, err = f.WriteString(content.String())
	if err != nil {
		return fmt.Errorf("error writing to file %s: %v", path, err)
	}

	return nil
}

// Order tasks by their id
func (g Graph) SequentialTasks() [][]Task {
	maxId := -1
	for _, nodes := range g.Tasks {
		for _, node := range nodes {
			if node.Id > maxId {
				maxId = node.Id
			}
		}
	}

	sortedTasks := make([][]Task, maxId+1)
	for _, nodes := range g.Tasks {
		for _, node := range nodes {
			sortedTasks[node.Id] = []Task{node}
		}
	}
	return sortedTasks
}

// Order tasks based on execution level
func (g Graph) ParallelTasks() [][]Task {
	return g.Tasks
}

func NewGraphFromFile(path string, rolesPaths string) (Graph, error) {
	// Read YAML file
	data, err := os.ReadFile(path)
	if err != nil {
		return Graph{}, fmt.Errorf("error reading YAML file %s: %v", path, err)
	}
	return NewGraphFromPlaybook(data, filepath.Dir(path), rolesPaths)
}

func NewGraphFromPlaybook(data []byte, basePath string, rolesPaths string) (Graph, error) {
	// Preprocess the playbook data with current directory as base path
	if basePath == "" {
		basePath = "."
	}

	// Split the roles paths from the configuration
	splitRolesPaths := func(rolesPaths string) []string {
		if rolesPaths == "" {
			return []string{"roles"} // Default to "roles" directory
		}
		paths := strings.Split(rolesPaths, ":")
		// Filter out empty paths
		var result []string
		for _, path := range paths {
			if strings.TrimSpace(path) != "" {
				result = append(result, strings.TrimSpace(path))
			}
		}
		if len(result) == 0 {
			return []string{"roles"} // Fallback to default if all paths are empty
		}
		return result
	}

	processedNodes, err := compile.PreprocessPlaybook(data, basePath, splitRolesPaths(rolesPaths))
	if err != nil {
		return Graph{}, fmt.Errorf("error preprocessing playbook data: %w", err)
	}

	// Parse YAML nodes into tasks
	attributes, err := ParsePlayAttributes(processedNodes)
	if err != nil {
		// We allow the graph to not have a root
		common.LogDebug("No root block found in playbook, using empty attributes", map[string]interface{}{"playbook": basePath})
		attributes = make(map[string]interface{})
	}
	tasks, err := TextToGraphNodes(processedNodes)
	if err != nil {
		return Graph{}, fmt.Errorf("error parsing preprocessed tasks: %w", err)
	}

	graph, err := NewGraph(tasks, attributes)
	if err != nil {
		return Graph{}, fmt.Errorf("failed to generate graph: %w", err)
	}
	common.LogDebug("NewGraph generated graph.", map[string]interface{}{"graph": graph.String()})
	return graph, nil
}

// Helper function to flatten GraphNodes into a list of TaskNodes
// while preserving a semblance of original order.
func flattenNodes(nodes []GraphNode) []Task {
	var flatTasks []Task
	var collectTasks func(node GraphNode)
	collectTasks = func(node GraphNode) {
		switch n := node.(type) {
		case Task:
			flatTasks = append(flatTasks, n)
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

func GetVariableUsage(task Task) ([]string, error) {
	varsUsage, err := GetVariableUsageFromModule(task.Params.Actual)
	if err != nil {
		return nil, fmt.Errorf("error getting variable usage from module: %w", err)
	}

	// Get variable usage from fields on the task itself.
	if task.Loop != nil {
		// TODO: change name of the variable if loopcontrol is used
		varsUsage = common.RemoveFromSlice(varsUsage, "item")
		if loop, ok := task.Loop.(string); ok {
			varsUsage = append(varsUsage, GetVariableUsageFromTemplate(loop)...)
		}
	}
	return varsUsage, nil
}

func NewGraph(nodes []GraphNode, graphAttributes map[string]interface{}) (Graph, error) {
	common.LogDebug("NewGraph received nodes.", map[string]interface{}{"count": len(nodes), "attributes": graphAttributes}) // Log input count
	if graphAttributes["vars"] == nil {
		graphAttributes["vars"] = make(map[string]interface{})
	}
	g := Graph{RequiredInputs: []string{}, Vars: graphAttributes["vars"].(map[string]interface{})}
	dependsOn := map[string][]string{}
	taskIdMapping := map[int]Task{}
	lastTaskNameMapping := map[string]Task{}
	originalIndexMap := map[string]int{} // Map task name to its original flattened index
	dependsOnVariables := map[string][]string{}
	variableProvidedBy := map[string]string{}
	visited := map[string]bool{}
	recStack := map[string]bool{}

	// 1. Flatten nodes and record original order index
	flattenedTasks := flattenNodes(nodes)

	// 1.5. Separate handlers from regular tasks
	var regularTasks []Task
	var handlers []Task
	for _, task := range flattenedTasks {
		if task.IsHandler {
			handlers = append(handlers, task)
		} else {
			regularTasks = append(regularTasks, task)
		}
	}

	// Store handlers in the graph
	g.Handlers = handlers

	// Process only regular tasks for the main execution flow
	for i, task := range regularTasks {
		if _, exists := lastTaskNameMapping[task.Name]; exists {
			// Handle potential duplicate task names if necessary, though Ansible usually requires unique names within a play
			common.LogWarn("Duplicate task name found during flattening", map[string]interface{}{"name": task.Name, "index": i})
			// For now, we'll overwrite, assuming later tasks with the same name take precedence or are errors
		}
		taskIdMapping[task.Id] = task
		lastTaskNameMapping[task.Name] = task
		originalIndexMap[task.Name] = i
	}

	// 2. Build dependencies based on flattened tasks
	for _, n := range taskIdMapping {
		common.DebugOutput("Processing node TaskNode %q %q: %+v", n.Name, n.Module, n.Params)
		// Check if n.Params.Actual is nil, as n.Params is a struct and cannot be nil itself.
		if n.Params.Actual == nil {
			common.DebugOutput("Task %q has no actual params, skipping dependency analysis for params", n.Name)
			// Continue processing other dependencies like before/after
		} else {
			vars, err := GetVariableUsage(n)
			if err != nil {
				return Graph{}, fmt.Errorf("error getting variable usage for task %q: %w", n.Name, err)
			}

			// Filter out variables that are provided by task-level vars
			filteredVars := []string{}
			if n.Vars != nil {
				if varsMap, ok := n.Vars.(map[string]interface{}); ok {
					taskVarNames := make(map[string]bool)
					for varName := range varsMap {
						taskVarNames[varName] = true
					}
					// Only include variables that are NOT provided by task-level vars
					for _, varName := range vars {
						if !taskVarNames[varName] {
							filteredVars = append(filteredVars, varName)
						}
					}
				} else {
					// If vars is not a map, use all variables
					filteredVars = vars
				}
			} else {
				// No task-level vars, use all variables
				filteredVars = vars
			}

			dependsOnVariables[n.Name] = filteredVars
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
		// Ensure n.Params.Actual is not nil before accessing its methods.
		if n.Params.Actual != nil {
			providedVars := n.Params.Actual.ProvidesVariables()
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

	// Get required inputs from graph attributes
	for _, v := range graphAttributes["vars"].(map[string]interface{}) {
		varsUsage := GetVariableUsageFromTemplate(v.(string))
		g.RequiredInputs = append(g.RequiredInputs, varsUsage...)
	}

	// 3. Check for cycles
	for taskName := range lastTaskNameMapping {
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
			// Skip vars that are provided by the playbook root
			if g.Vars[varName] != nil {
				continue
			}

			providingTask, ok := variableProvidedBy[varName]
			if !ok {
				common.DebugOutput("no task found that provides variable %q for task %q", varName, taskName)
				// Don't add facts that will be gathered by setup module to RequiredInputs
				if _, isGatherableFact := AllowedFacts[varName]; !isGatherableFact {
					if !containsInSlice(g.RequiredInputs, varName) {
						g.RequiredInputs = append(g.RequiredInputs, varName)
					}
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
	executedOnStep := map[int]int{}
	allTaskIds := make([]int, 0, len(taskIdMapping))
	for taskId := range taskIdMapping {
		allTaskIds = append(allTaskIds, taskId)
	}
	// Sort task IDs to ensure deterministic processing order for ResolveExecutionLevel
	// This helps make the *level assignment* itself more stable, although the final sort step is the primary guarantee.
	sort.Ints(allTaskIds)
	for _, taskId := range allTaskIds {
		task := taskIdMapping[taskId]
		if _, processed := executedOnStep[task.Id]; !processed {
			executedOnStep = ResolveExecutionLevel(task, lastTaskNameMapping, dependsOn, executedOnStep)
		}
	}

	// 6. Determine max execution level and initialize task slices
	var maxExecutionLevel int
	for _, executionLevel := range executedOnStep {
		maxExecutionLevel = max(maxExecutionLevel, executionLevel)
	}

	// 6.5. Determine if we need to inject a gather facts task and create it
	usedFacts := make(map[string]struct{})
	for _, task := range taskIdMapping {
		vars, _ := GetVariableUsage(task)
		for _, v := range vars {
			if _, ok := AllowedFacts[v]; ok {
				usedFacts[v] = struct{}{}
			}
		}
	}

	// Filter out required inputs that are facts and add them to usedFacts
	filteredInputs := make([]string, 0, len(g.RequiredInputs))
	for _, input := range g.RequiredInputs {
		if _, ok := AllowedFacts[input]; ok {
			usedFacts[input] = struct{}{}
		} else {
			filteredInputs = append(filteredInputs, input)
		}
	}
	g.RequiredInputs = filteredInputs

	factList := make([]string, 0, len(usedFacts))
	for fct := range usedFacts {
		factList = append(factList, fct)
	}

	hasSetupTask := len(factList) > 0

	// If we need a setup task, adjust the execution levels
	if hasSetupTask {
		// Increment all execution levels by 1 to make room for the setup task at level 0
		for taskName := range executedOnStep {
			executedOnStep[taskName]++
		}
		maxExecutionLevel++
	}

	g.Tasks = make([][]Task, maxExecutionLevel+1)
	for i := range g.Tasks {
		g.Tasks[i] = []Task{}
	}

	// 7. Populate tasks into levels
	// First, add the setup task if needed
	if hasSetupTask {
		// Use reflection to create SetupInput to avoid import cycle
		setupModule, ok := GetModule("setup")
		if !ok {
			return Graph{}, fmt.Errorf("setup module not found")
		}

		// Create SetupInput using reflection
		setupInputType := setupModule.InputType()
		setupInputValue := reflect.New(setupInputType).Elem()

		// Set the Facts field using reflection
		factsField := setupInputValue.FieldByName("Facts")
		if !factsField.IsValid() {
			return Graph{}, fmt.Errorf("Facts field not found in SetupInput")
		}
		factsField.Set(reflect.ValueOf(factList))

		// Convert to ConcreteModuleInputProvider
		setupInputProvider, ok := setupInputValue.Interface().(ConcreteModuleInputProvider)
		if !ok {
			return Graph{}, fmt.Errorf("SetupInput does not implement ConcreteModuleInputProvider")
		}

		setupTask := Task{
			Id:       0,
			Name:     "gather facts",
			Module:   "setup",
			Register: "ansible_facts",
			Params: ModuleInput{
				Actual: setupInputProvider,
			},
			RunAs: "",
			When:  "",
		}
		g.Tasks[0] = []Task{setupTask}

		// Register that the setup task provides facts
		variableProvidedBy["ansible_facts"] = "gather facts"
		for _, fact := range factList {
			variableProvidedBy[fact] = "gather facts"
		}
	}

	// Then add the regular tasks with potentially incremented IDs
	for taskId, executionLevel := range executedOnStep {
		task := taskIdMapping[taskId]
		// Increment task ID by 1 if we have a setup task
		if hasSetupTask {
			task.Id = task.Id + 1
		}
		g.Tasks[executionLevel] = append(g.Tasks[executionLevel], task)
	}

	// 8. Sort tasks within each level based on original index
	for i := range g.Tasks {
		sort.SliceStable(g.Tasks[i], func(a, b int) bool {
			taskA := g.Tasks[i][a]
			taskB := g.Tasks[i][b]
			// Compare based on the original flattened index
			return originalIndexMap[taskA.Name] < originalIndexMap[taskB.Name]
		})
	}

	common.LogDebug("NewGraph finished building.", map[string]interface{}{"graph": g.String()}) // Log final graph string
	return g, nil
}

func ResolveExecutionLevel(task Task, taskNameMapping map[string]Task, dependsOn map[string][]string, executedOnStep map[int]int) map[int]int {
	if len(dependsOn[task.Name]) == 0 {
		executedOnStep[task.Id] = 0
		return executedOnStep
	}
	for _, parentTaskName := range dependsOn[task.Name] {
		parentTask := taskNameMapping[parentTaskName]
		executedOnStep = ResolveExecutionLevel(parentTask, taskNameMapping, dependsOn, executedOnStep)
		executedOnStep[task.Id] = max(executedOnStep[task.Id], executedOnStep[parentTask.Id]+1)
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

func (g Graph) SaveToTemporalWorkflowFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("error creating directories: %v", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("error creating file %s: %v", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			common.LogWarn("Failed to close file", map[string]interface{}{
				"file":  path,
				"error": closeErr.Error(),
			})
		}
	}()

	common.LogInfo("Generating Temporal workflow runner Go code", map[string]interface{}{
		"path": path,
	})

	graphCode := g.ToCode()

	var content strings.Builder
	content.WriteString(`package main

import (
	"os"

	"github.com/AlexanderGrooff/spage/cmd"
	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/modules"
)

`)

	// Inject the GeneratedGraph definition
	content.WriteString("\n")      // Ensure a newline before graph code
	content.WriteString(graphCode) // graphCode produces "var GeneratedGraph = pkg.Graph{...}"
	content.WriteString("\n")      // Ensure a newline after graph code

	content.WriteString(`func main() {
	err := cmd.EntrypointTemporalExecutor(GeneratedGraph)
	if err != nil {
		common.LogError("Failed to run playbook", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}
}
`)

	_, err = file.WriteString(content.String())
	if err != nil {
		return fmt.Errorf("error writing to file %s: %v", path, err)
	}

	common.LogInfo("Temporal workflow runner Go code generated successfully", map[string]interface{}{"path": path})
	return nil
}
