package pkg

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/compile"
	"github.com/AlexanderGrooff/spage/pkg/config"
)

// Don't look for dependencies for these vars
var SpecialVars = []string{
	"previous", // Provided in 'revert' context
}

// Facts that can be gathered by the setup module
var AllowedFacts = map[string]struct{}{
	"platform":                           {},
	"user":                               {},
	"inventory_hostname":                 {},
	"ssh_host_pub_keys":                  {},
	"inventory_hostname_short":           {},
	"ansible_distribution":               {},
	"ansible_distribution_major_version": {},
}

// GraphNode represents either a list of tasks or a nested graph
type GraphNode interface {
	String() string
	ToCode() string
	GetVariableUsage() ([]string, error)
	ConstructClosure(c *HostContext, cfg *config.Config) *Closure
	ExecuteModule(closure *Closure) TaskResult
	RevertModule(closure *Closure) TaskResult

	// Getters
	GetId() int
	SetId(id int)
	GetName() string
	GetIsHandler() bool
	GetTags() []string
}

type Graph struct {
	RequiredInputs []string
	Nodes          [][]GraphNode
	Handlers       []GraphNode
	Vars           map[string]interface{}
	PlaybookPath   string
}

func (g Graph) String() string {
	var b strings.Builder
	for i, node := range g.Nodes {
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

	for _, node := range g.Nodes {
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
	localCmd := cmd.NewLocalExecutorCmd(GeneratedGraph)
	temporalCmd := cmd.NewTemporalExecutorCmd(GeneratedGraph)

	rootCmd := localCmd
	rootCmd.AddCommand(temporalCmd)

	if err := rootCmd.Execute(); err != nil {
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
func (g Graph) SequentialTasks() [][]GraphNode {
	maxId := -1
	for _, nodes := range g.Nodes {
		for _, node := range nodes {
			if node.GetId() > maxId {
				maxId = node.GetId()
			}
		}
	}

	sortedTasks := make([][]GraphNode, maxId+1)
	for _, nodes := range g.Nodes {
		for _, node := range nodes {
			sortedTasks[node.GetId()] = []GraphNode{node}
		}
	}
	return sortedTasks
}

// Order tasks based on execution level
func (g Graph) ParallelTasks() [][]GraphNode {
	return g.Nodes
}

func NewGraphFromFile(playbookPath string, rolesPaths string) (Graph, error) {
	// Read YAML file
	absPlaybookPath, err := filepath.Abs(playbookPath)
	if err != nil {
		return Graph{}, fmt.Errorf("error getting absolute path for playbook %s: %v", playbookPath, err)
	}
	data, err := os.ReadFile(absPlaybookPath)
	if err != nil {
		return Graph{}, fmt.Errorf("error reading YAML file %s: %v", absPlaybookPath, err)
	}

	currCwd, err := ChangeCWDToPlaybookDir(absPlaybookPath)
	if err != nil {
		return Graph{}, fmt.Errorf("error changing directory to playbook path %s: %v", absPlaybookPath, err)
	}
	defer func() {
		if err := os.Chdir(currCwd); err != nil {
			common.LogWarn("failed to change directory back to %s: %v", map[string]interface{}{"path": currCwd, "error": err.Error()})
		}
	}()

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

	// Base path is set to '.' because we changed the cwd to the playbook path already
	processedNodes, err := compile.PreprocessPlaybook(data, ".", splitRolesPaths(rolesPaths))
	if err != nil {
		return Graph{}, fmt.Errorf("error preprocessing playbook data: %w", err)
	}

	// Parse YAML nodes into tasks
	attributes, err := ParsePlayAttributes(processedNodes)
	if err != nil {
		// We allow the graph to not have a root
		attributes = make(map[string]interface{})
	}
	tasks, err := TextToGraphNodes(processedNodes)
	if err != nil {
		return Graph{}, fmt.Errorf("error parsing preprocessed tasks: %w", err)
	}

	graph, err := NewGraph(tasks, attributes, absPlaybookPath)
	if err != nil {
		return Graph{}, fmt.Errorf("failed to generate graph: %w", err)
	}
	return graph, nil
}

// NewGraphFromFS builds a graph from a playbook path within an fs.FS.
// The playbookPath must be the POSIX-style path inside the provided FS.
func NewGraphFromFS(sourceFS fs.FS, playbookPath string, rolesPaths string) (Graph, error) {
	// Read playbook YAML from FS
	data, err := fs.ReadFile(sourceFS, playbookPath)
	if err != nil {
		return Graph{}, fmt.Errorf("error reading YAML from FS %s: %v", playbookPath, err)
	}

	// Split roles paths from configuration
	splitRolesPaths := func(rolesPaths string) []string {
		if rolesPaths == "" {
			return []string{"roles"}
		}
		paths := strings.Split(rolesPaths, ":")
		var result []string
		for _, p := range paths {
			if strings.TrimSpace(p) != "" {
				result = append(result, strings.TrimSpace(p))
			}
		}
		if len(result) == 0 {
			return []string{"roles"}
		}
		return result
	}

	// Use FS-aware preprocessing. Base path is directory of the playbook inside FS.
	basePath := filepath.ToSlash(filepath.Dir(playbookPath))
	processedNodes, err := compile.PreprocessPlaybookFS(sourceFS, data, basePath, splitRolesPaths(rolesPaths))
	if err != nil {
		return Graph{}, fmt.Errorf("error preprocessing playbook data from FS: %w", err)
	}

	attributes, err := ParsePlayAttributes(processedNodes)
	if err != nil {
		attributes = make(map[string]interface{})
	}
	// Parse YAML nodes into tasks
	tasks, err := TextToGraphNodes(processedNodes)
	if err != nil {
		return Graph{}, fmt.Errorf("error parsing preprocessed tasks: %w", err)
	}

	// Use playbookPath as-is to avoid OS cwd changes; executor must handle FS mode.
	graph, err := NewGraph(tasks, attributes, playbookPath)
	if err != nil {
		return Graph{}, fmt.Errorf("failed to generate graph: %w", err)
	}
	return graph, nil
}

func NewGraph(nodes []GraphNode, graphAttributes map[string]interface{}, playbookPath string) (Graph, error) {
	if graphAttributes["vars"] == nil {
		graphAttributes["vars"] = make(map[string]interface{})
	}
	g := Graph{RequiredInputs: []string{}, Vars: graphAttributes["vars"].(map[string]interface{}), PlaybookPath: playbookPath}
	dependsOn := map[string][]string{}
	taskIdMapping := map[int]GraphNode{}
	lastTaskNameMapping := map[string]GraphNode{}
	originalIndexMap := map[string]int{} // Map task name to its original flattened index
	dependsOnVariables := map[string][]string{}
	variableProvidedBy := map[string]string{}
	visited := map[string]bool{}
	recStack := map[string]bool{}

	// 1. Flatten nodes and record original order index
	flattenedTasks := nodes

	// 1.5. Separate handlers from regular tasks
	var regularTasks []GraphNode
	var handlers []GraphNode
	for _, task := range flattenedTasks {
		if task.GetIsHandler() {
			handlers = append(handlers, task)
		} else {
			regularTasks = append(regularTasks, task)
		}
	}

	// Store handlers in the graph
	g.Handlers = handlers

	// Process only regular tasks for the main execution flow
	for i, task := range regularTasks {
		if _, exists := lastTaskNameMapping[task.GetName()]; exists {
			// Handle potential duplicate task names if necessary, though Ansible usually requires unique names within a play
			common.LogWarn("Duplicate task name found during flattening", map[string]interface{}{"name": task.GetName(), "index": i})
			// For now, we'll overwrite, assuming later tasks with the same name take precedence or are errors
		}
		taskIdMapping[task.GetId()] = task
		lastTaskNameMapping[task.GetName()] = task
		originalIndexMap[task.GetName()] = i
	}

	// 2. Build dependencies based on flattened tasks
	for _, gn := range taskIdMapping {
		// TODO: Handle TaskCollection
		t, ok := gn.(*Task)
		if !ok {
			continue
		}

		common.DebugOutput("Processing node TaskNode %q %q: %+v", t.Name, t.Module, t.Params)
		// Check if n.Params.Actual is nil, as n.Params is a struct and cannot be nil itself.
		if t.Params.Actual == nil {
			common.DebugOutput("Task %q has no actual params, skipping dependency analysis for params", t.Name)
			// Continue processing other dependencies like before/after
		} else {
			vars, err := t.GetVariableUsage()
			if err != nil {
				return Graph{}, fmt.Errorf("error getting variable usage for task %q: %w", t.Name, err)
			}

			// Filter out variables that are provided by task-level vars
			filteredVars := []string{}
			if t.Vars != nil {
				if varsMap, ok := t.Vars.(map[string]interface{}); ok {
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

			dependsOnVariables[t.Name] = filteredVars
		}

		if t.Before != "" {
			// Task 'n' must run *before* task 'n.Before'
			// So, 'n.Before' depends on 'n'
			dependsOn[t.Before] = append(dependsOn[t.Before], t.Name)
		}
		if t.After != "" {
			// Task 'n' must run *after* task 'n.After'
			// So, 'n' depends on 'n.After'
			dependsOn[t.Name] = append(dependsOn[t.Name], t.After)
		}
		if t.Register != "" {
			variableProvidedBy[strings.ToLower(t.Register)] = t.Name
		}

		// Check if the module's parameters inherently provide variables (like set_fact)
		// Ensure n.Params.Actual is not nil before accessing its methods.
		if t.Params.Actual != nil {
			providedVars := t.Params.Actual.ProvidesVariables()
			for _, providedVar := range providedVars {
				// TODO: Consider case sensitivity/normalization if needed (Ansible usually lowercases)
				variableProvidedBy[providedVar] = t.Name
			}
		}
	}

	// TODO: does this do anything?
	// // Extract required inputs from nested graphs (if any were present in the original structure)
	// var collectRequiredInputs func(node GraphNode)
	// collectRequiredInputs = func(node GraphNode) {
	// 	switch n := node.(type) {
	// 	case Graph:
	// 		for _, input := range n.RequiredInputs {
	// 			if !containsInSlice(g.RequiredInputs, input) {
	// 				g.RequiredInputs = append(g.RequiredInputs, input)
	// 			}
	// 		}
	// 		// Recursively check nested graphs
	// 		for _, step := range n.Nodes {
	// 			for _, subNode := range step {
	// 				collectRequiredInputs(subNode)
	// 			}
	// 		}
	// 		// Ignore TaskNode and Task types for required inputs collection
	// 	}
	// }
	// for _, node := range nodes { // Iterate original nodes structure for nested graph inputs
	// 	collectRequiredInputs(node)
	// }

	// Get required inputs from graph attributes
	for _, v := range graphAttributes["vars"].(map[string]interface{}) {
		vStr, ok := v.(string)
		if !ok {
			// It's not a string, so it's not a variable
			continue
		}
		varsUsage := GetVariableUsageFromTemplate(vStr)
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
		if _, processed := executedOnStep[task.GetId()]; !processed {
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
		vars, _ := task.GetVariableUsage()
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

	g.Nodes = make([][]GraphNode, maxExecutionLevel+1)
	for i := range g.Nodes {
		g.Nodes[i] = []GraphNode{}
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
			BecomeUser: "",
			When:       JinjaExpressionList{},
		}
		g.Nodes[0] = []GraphNode{&setupTask}

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
			task.SetId(task.GetId() + 1)
		}
		g.Nodes[executionLevel] = append(g.Nodes[executionLevel], task)
	}

	// 8. Sort tasks within each level based on original index
	for i := range g.Nodes {
		sort.SliceStable(g.Nodes[i], func(a, b int) bool {
			taskA := g.Nodes[i][a]
			taskB := g.Nodes[i][b]
			// Compare based on the original flattened index
			return originalIndexMap[taskA.GetName()] < originalIndexMap[taskB.GetName()]
		})
	}

	return g, nil
}

func ResolveExecutionLevel(task GraphNode, taskNameMapping map[string]GraphNode, dependsOn map[string][]string, executedOnStep map[int]int) map[int]int {
	if len(dependsOn[task.GetName()]) == 0 {
		executedOnStep[task.GetId()] = 0
		return executedOnStep
	}
	for _, parentTaskName := range dependsOn[task.GetName()] {
		parentTask := taskNameMapping[parentTaskName]
		executedOnStep = ResolveExecutionLevel(parentTask, taskNameMapping, dependsOn, executedOnStep)
		executedOnStep[task.GetId()] = max(executedOnStep[task.GetId()], executedOnStep[parentTask.GetId()]+1)
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

func (g Graph) CheckForRequiredInputs(hostContexts map[string]*HostContext) error {
	missingInputs := []string{}
	for _, hostContext := range hostContexts {
		for _, input := range g.RequiredInputs {
			// All other required inputs should be present in the inventory
			if _, ok := hostContext.Facts.Load(input); !ok {
				missingInputs = append(missingInputs, input)
			}
		}
	}
	if len(missingInputs) > 0 {
		return fmt.Errorf("required inputs %v not found in host contexts", missingInputs)
	}
	return nil
}
