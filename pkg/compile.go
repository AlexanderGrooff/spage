package pkg

import (
	"fmt"
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

		// *** Generic Module Alias Handling Start ***
		// Before marshaling/unmarshaling into specific type, handle parameter aliases
		if aliases := module.ParameterAliases(); aliases != nil {
			if paramsMap, ok := moduleParams.(map[string]interface{}); ok {
				modified := false
				for aliasName, canonicalName := range aliases {
					if _, canonicalExists := paramsMap[canonicalName]; !canonicalExists {
						if aliasVal, aliasExists := paramsMap[aliasName]; aliasExists {
							common.DebugOutput("Promoting alias %q to %q for module %s task %q", aliasName, canonicalName, task.Module, task.Name)
							paramsMap[canonicalName] = aliasVal
							delete(paramsMap, aliasName) // Remove the alias
							modified = true
						}
					}
				}
				// Update moduleParams map reference only if it was modified
				if modified {
					moduleParams = paramsMap
				}
			}
		} // Else: module doesn't define aliases or params aren't a map
		// *** Generic Module Alias Handling End ***

		// Convert back to yaml so we can unmarshal it into the correct type
		paramsData, err := yaml.Marshal(moduleParams)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to marshal params for module %s: %v", task.Module, moduleParams))
			continue
		}

		// Now we can unmarshal the params into the correct type.
		// If task.Module is "shell", this will invoke the custom UnmarshalYAML in shell.go
		params := reflect.New(module.InputType()).Interface()
		if err := yaml.Unmarshal(paramsData, params); err != nil {
			// This error should now only happen for genuinely invalid map structures
			// or if the custom unmarshaler in a module (like shell) returned an error.
			errors = append(errors, fmt.Errorf("failed to unmarshal params for module %s: %w", task.Module, err))
			continue
		}

		// Validate that there are no extra keys in the block, only if params was originally a map
		// Check the original moduleParams type, not the result of unmarshaling
		if _, isMap := moduleParams.(map[string]interface{}); isMap {
			structType := module.InputType()
			structFields := make(map[string]struct{})
			// Collect field names from the struct
			for i := 0; i < structType.NumField(); i++ {
				field := structType.Field(i)
				// Use the yaml tag name for matching keys
				yamlTag := field.Tag.Get("yaml")
				if yamlTag != "" && yamlTag != "-" { // Ignore fields without tags or explicitly ignored
					// Handle tags like "field,omitempty"
					tagName := strings.Split(yamlTag, ",")[0]
					structFields[tagName] = struct{}{}
				}
			}

			// Check for extra keys in the module block map
			paramsBlock := moduleParams.(map[string]interface{}) // We already know it's a map
			for k := range paramsBlock {
				if _, ok := structFields[k]; !ok {
					errors = append(errors, fmt.Errorf("extra key %q found in params for task %q", k, task.Name))
				}
			}
		} // else: If it wasn't originally a map (e.g., string shorthand handled above), skip key validation.

		// Ensure params is of the correct type using reflection
		// We need to get the value pointed to by params, as it's a pointer receiver from reflect.New
		paramsValue := reflect.ValueOf(params).Elem().Interface()
		if typedParams, ok := paramsValue.(ModuleInput); ok {
			task.Params = typedParams
		} else {
			errors = append(errors, fmt.Errorf("params (%T) do not implement ModuleInput for module %s", paramsValue, task.Module))
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
