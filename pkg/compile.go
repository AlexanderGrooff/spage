package pkg

import (
	"fmt"
	"reflect"
	"strings"

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

func TextToGraphNodes(text []byte) ([]GraphNode, error) {
	var yamlMap []map[string]interface{}
	err := yaml.Unmarshal([]byte(text), &yamlMap)
	if err != nil {
		return nil, err
	}
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

	for _, block := range yamlMap {
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

		// Handle include module during compilation
		if task.Module == "include" {
			path, ok := paramsBlock["path"].(string)
			if !ok {
				errors = append(errors, fmt.Errorf("include module requires a path"))
				continue
			}
			nestedGraph, err := NewGraphFromFile(path)
			if err != nil {
				errors = append(errors, fmt.Errorf("failed to parse included graph from %s: %v", path, err))
				continue
			}
			tasks = append(tasks, nestedGraph)
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
			switch n := node.(type) {
			case Task:
				// Replace variables in task fields
				task := n
				// Get the value struct of the task
				v := reflect.ValueOf(&task).Elem()

				// Iterate through all fields of the task
				for i := 0; i < v.NumField(); i++ {
					field := v.Field(i)
					// Only process string fields
					if field.Kind() == reflect.String {
						// Get the string value and template it
						strVal := field.String()
						if templated, err := TemplateString(strVal, host.Vars); err == nil {
							field.SetString(templated)
						}
					}
				}

				// TODO: Template the params from inventory into the tasks if they exist
				compiledGraph.Tasks[i][j] = task
			case Graph:
				// Recursively compile nested graphs
				compiledNestedGraph, err := CompileGraphForHost(n, host)
				if err != nil {
					return Graph{}, fmt.Errorf("failed to compile nested graph: %w", err)
				}
				compiledGraph.Tasks[i][j] = compiledNestedGraph
			default:
				return Graph{}, fmt.Errorf("unknown node type: %T", node)
			}
		}
	}

	return compiledGraph, nil
}
