package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"gopkg.in/yaml.v3"
)

func Indent(n int) string {
	if n == 0 {
		return ""
	}
	return "  " + Indent(n-1)
}

func containsInMap(m map[string]interface{}, item string) bool {
	for k := range m {
		if k == item {
			return true
		}
	}
	return false
}

func containsInSlice(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func getStringFromMap(m map[string]interface{}, key string) string {
	if value, ok := m[key]; ok {
		return value.(string)
	}
	return ""
}

// processIncludeDirective handles the 'include' directive during preprocessing.
func processIncludeDirective(includeValue interface{}, currentBasePath string) ([]map[string]interface{}, error) {
	if pathStr, ok := includeValue.(string); ok {
		absPath := pathStr
		if !filepath.IsAbs(pathStr) {
			absPath = filepath.Join(currentBasePath, pathStr)
		}
		includedData, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read included file %s: %w", absPath, err)
		}
		// Recursively preprocess the included content, using the included file's directory as the new base path
		nestedBasePath := filepath.Dir(absPath)
		nestedBlocks, err := preprocessPlaybook(includedData, nestedBasePath)
		if err != nil {
			return nil, fmt.Errorf("failed to preprocess included file %s: %w", absPath, err)
		}
		return nestedBlocks, nil
	} else {
		return nil, fmt.Errorf("invalid 'include' value: expected string, got %T", includeValue)
	}
}

// processIncludeRoleDirective handles the 'include_role' directive during preprocessing.
func processIncludeRoleDirective(roleParams interface{}, currentBasePath string) ([]map[string]interface{}, error) {
	paramsMap, ok := roleParams.(map[string]interface{})
	if !ok {
		// Handle simple string form: include_role: my_role_name
		if roleNameStr, okStr := roleParams.(string); okStr {
			paramsMap = map[string]interface{}{"name": roleNameStr}
			ok = true
		} else {
			return nil, fmt.Errorf("invalid 'include_role' value: expected map or string, got %T", roleParams)
		}
	}

	roleName, nameOk := paramsMap["name"].(string)
	if !nameOk || roleName == "" {
		return nil, fmt.Errorf("missing or invalid 'name' in include_role directive")
	}

	// Assume roles are in a 'roles' directory relative to the current base path.
	// TODO: Make roles path configurable.
	roleTasksPath := filepath.Join(currentBasePath, "roles", roleName, "tasks", "main.yml")

	roleData, err := os.ReadFile(roleTasksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("role tasks file not found: %s", roleTasksPath)
		} else {
			return nil, fmt.Errorf("failed to read role tasks file %s: %w", roleTasksPath, err)
		}
	}

	// Recursively preprocess the role's tasks, using the role's tasks directory as the base path
	roleTasksBasePath := filepath.Dir(roleTasksPath)
	roleBlocks, err := preprocessPlaybook(roleData, roleTasksBasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to preprocess role '%s' tasks from %s: %w", roleName, roleTasksPath, err)
	}
	return roleBlocks, nil
}

// preprocessPlaybook takes raw playbook YAML data and a base path,
// recursively processes include/include_role directives, and returns a flattened
// list of raw task maps ready for parsing.
func preprocessPlaybook(data []byte, basePath string) ([]map[string]interface{}, error) {
	var initialBlocks []map[string]interface{}
	err := yaml.Unmarshal(data, &initialBlocks)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling YAML: %w", err)
	}

	var processedBlocks []map[string]interface{}
	var processErrors []error

	for _, block := range initialBlocks {
		if includeValue, isInclude := block["include"]; isInclude {
			nestedBlocks, err := processIncludeDirective(includeValue, basePath)
			if err != nil {
				processErrors = append(processErrors, err)
				continue
			}
			processedBlocks = append(processedBlocks, nestedBlocks...)
		} else if roleParams, isIncludeRole := block["include_role"]; isIncludeRole {
			nestedBlocks, err := processIncludeRoleDirective(roleParams, basePath)
			if err != nil {
				processErrors = append(processErrors, err)
				continue
			}
			processedBlocks = append(processedBlocks, nestedBlocks...)
		} else {
			// Assume it's a standard task block
			processedBlocks = append(processedBlocks, block)
		}
	}

	if len(processErrors) > 0 {
		errorMessages := make([]string, len(processErrors))
		for i, e := range processErrors {
			errorMessages[i] = e.Error()
		}
		return nil, fmt.Errorf("errors during preprocessing:\n%s", strings.Join(errorMessages, "\n"))
	}

	return processedBlocks, nil
}

func TextToGraphNodes(blocks []map[string]interface{}) ([]GraphNode, error) {
	arguments := []string{
		"name",
		"validate",
		"before",
		"after",
		"when",
		"register",
		"run_as",
	}

	var tasks []GraphNode
	var errors []error

	for _, block := range blocks {
		task := Task{
			Name:     getStringFromMap(block, "name"),
			Validate: getStringFromMap(block, "validate"),
			Before:   getStringFromMap(block, "before"),
			After:    getStringFromMap(block, "after"),
			When:     getStringFromMap(block, "when"),
			Register: getStringFromMap(block, "register"),
			RunAs:    getStringFromMap(block, "run_as"),
		}

		var module Module
		var moduleParams interface{}
		var errored bool
		for k, v := range block {
			if !containsInSlice(arguments, k) {
				if m, ok := GetModule(k); ok {
					task.Module = k
					module = m
					moduleParams = v
					break
				} else {
					errors = append(errors, fmt.Errorf("unknown key %q in task %q", k, task.Name))
					errored = true
				}
			}
		}
		if errored {
			continue
		}

		// Convert back to yaml so we can unmarshal it into the correct type
		paramsData, err := yaml.Marshal(moduleParams)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to marshal params for module %s: %v", task.Module, moduleParams))
			continue
		}

		// Now we can unmarshal the params into the correct type
		params := reflect.New(module.InputType()).Interface()
		if err := yaml.Unmarshal(paramsData, params); err != nil {
			errors = append(errors, fmt.Errorf("failed to unmarshal params for module %s: %v", task.Module, moduleParams))
			continue
		}

		// Validate that there are no extra keys in the block
		structType := module.InputType()
		structFields := make(map[string]struct{})

		// Collect field names from the struct
		for i := 0; i < structType.NumField(); i++ {
			field := structType.Field(i)
			structFields[field.Tag.Get("yaml")] = struct{}{}
		}

		// Check for extra keys in the module block
		paramsBlock, ok := block[task.Module].(map[string]interface{})
		if !ok {
			errors = append(errors, fmt.Errorf("params block is not a map for task %q: %v", task.Name, block[task.Module]))
			continue
		}
		for k := range paramsBlock {
			if _, ok := structFields[k]; !ok {
				errors = append(errors, fmt.Errorf("extra key %q found in params for task %q", k, task.Name))
			}
		}

		// Ensure params is of the correct type using reflection
		if typedParams, ok := params.(ModuleInput); ok {
			task.Params = typedParams
		} else {
			errors = append(errors, fmt.Errorf("params do not implement ModuleInput for module %s", task.Module))
			continue
		}

		tasks = append(tasks, task)
	}

	if len(errors) > 0 {
		errorMessages := make([]string, len(errors))
		for i, err := range errors {
			errorMessages[i] = err.Error()
		}
		return nil, fmt.Errorf("encountered errors:\n%s", strings.Join(errorMessages, "\n"))
	}

	return tasks, nil
}

func CompilePlaybookForHost(graph Graph, inventoryFile, hostname string) (Graph, error) {
	inventory, err := LoadInventory(inventoryFile)
	if err != nil {
		return Graph{}, fmt.Errorf("failed to load inventory: %w", err)
	}
	host, ok := inventory.Hosts[hostname]
	if !ok {
		return Graph{}, fmt.Errorf("host not found in inventory: %s", hostname)
	}

	return CompileGraphForHost(graph, *host)
}

// Compile the graph for a specific host by replacing variables with host-specific values.
// This is useful for generating a binary for a specific host, where it can be used directly
// without the need of an inventory file. It's as simple as downloading the binary and running it.
func CompileGraphForHost(graph Graph, host Host) (Graph, error) {
	// Create a copy of the graph to avoid modifying the original
	compiledGraph := Graph{
		RequiredInputs: graph.RequiredInputs,
		Tasks:          make([][]GraphNode, len(graph.Tasks)),
	}

	// Replace variables in each task with host-specific values
	for i, taskLayer := range graph.Tasks {
		compiledGraph.Tasks[i] = make([]GraphNode, len(taskLayer))
		for j, node := range taskLayer {
			compiledNode, err := compileNode(node, host)
			if err != nil {
				return Graph{}, fmt.Errorf("failed to compile node: %w", err)
			}
			compiledGraph.Tasks[i][j] = compiledNode
		}
	}

	return compiledGraph, nil
}

// Helper function to convert map[string]interface{} to *sync.Map
func MapToSyncMap(m map[string]interface{}) *sync.Map {
	sm := new(sync.Map)
	for k, v := range m {
		sm.Store(k, v)
	}
	return sm
}

// compileNode handles compilation of a single graph node, replacing variables with host values
func compileNode(node GraphNode, host Host) (GraphNode, error) {
	switch n := node.(type) {
	case Task:
		task := n
		v := reflect.ValueOf(&task).Elem()

		// Convert host.Vars to *sync.Map once for efficiency
		hostVarsSyncMap := MapToSyncMap(host.Vars)

		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.Kind() == reflect.String {
				strVal := field.String()
				// Pass the converted *sync.Map to TemplateString
				if templated, err := TemplateString(strVal, hostVarsSyncMap); err == nil {
					// Check if templating actually changed the value before setting
					// This avoids unnecessary reflection sets if the string doesn't contain variables
					if templated != strVal {
						field.SetString(templated)
					}
				} else {
					// Log or handle templating errors if necessary
					common.LogWarn("Templating failed for field", map[string]interface{}{
						"task":  task.Name,
						"field": v.Type().Field(i).Name,
						"value": strVal,
						"error": err.Error(),
					})
					// Decide if a templating error should halt compilation
					// return nil, fmt.Errorf("templating failed for task %s field %s: %w", task.Name, v.Type().Field(i).Name, err)
				}
			}
		}
		// TODO: Template the params from inventory into the tasks if they exist
		return task, nil

	case Graph:
		compiledNestedGraph, err := CompileGraphForHost(n, host)
		if err != nil {
			return nil, fmt.Errorf("failed to compile nested graph: %w", err)
		}
		return compiledNestedGraph, nil
	case TaskNode:
		// Assuming TaskNode just wraps a Task, compile the inner Task
		compiledInnerTask, err := compileNode(n.Task, host)
		if err != nil {
			return nil, fmt.Errorf("failed to compile task within TaskNode: %w", err)
		}
		// Need to ensure the returned type is TaskNode wrapping the compiled task
		if compiledTask, ok := compiledInnerTask.(Task); ok {
			return TaskNode{Task: compiledTask}, nil
		}
		return nil, fmt.Errorf("compiling TaskNode did not return a Task")

	default:
		return nil, fmt.Errorf("unknown node type: %T", node)
	}
}
