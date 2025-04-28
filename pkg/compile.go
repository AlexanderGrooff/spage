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
		"ignore_errors",
	}

	var tasks []GraphNode
	var errors []error

	for _, block := range blocks {
		task := Task{
			Name:     getStringFromMap(block, "name"),
			Validate: getStringFromMap(block, "validate"),
			Before:   getStringFromMap(block, "before"),
			After:    getStringFromMap(block, "after"),
			Register: getStringFromMap(block, "register"),
			RunAs:    getStringFromMap(block, "run_as"),
		}

		// Declare errored flag here
		var errored bool

		// Handle 'when' specifically to allow boolean values from YAML
		if whenVal, ok := block["when"]; ok {
			switch v := whenVal.(type) {
			case string:
				task.When = v
			case bool:
				// Convert boolean directly to string "true" or "false"
				task.When = fmt.Sprintf("%t", v)
			default:
				// Handle other types if necessary, or error out
				errors = append(errors, fmt.Errorf("invalid type (%T) for 'when' key in task %q, expected string or boolean", whenVal, task.Name))
				errored = true // Mark as errored to skip further processing of this task
			}
		} else {
			task.When = "" // Default if 'when' key is not present
		}

		// Handle 'ignore_errors'
		if ignoreVal, ok := block["ignore_errors"]; ok {
			switch v := ignoreVal.(type) {
			case bool:
				task.IgnoreErrors = v
			case string:
				// Attempt to parse boolean string, default to false if invalid
				if strings.ToLower(v) == "true" || strings.ToLower(v) == "yes" {
					task.IgnoreErrors = true
				} else if strings.ToLower(v) == "false" || strings.ToLower(v) == "no" {
					task.IgnoreErrors = false
				} else {
					errors = append(errors, fmt.Errorf("invalid string value (%q) for 'ignore_errors' key in task %q, expected 'true'/'yes' or 'false'/'no'", v, task.Name))
					errored = true
				}
			default:
				errors = append(errors, fmt.Errorf("invalid type (%T) for 'ignore_errors' key in task %q, expected boolean or boolean-like string", ignoreVal, task.Name))
				errored = true
			}
		} else {
			task.IgnoreErrors = false // Default if 'ignore_errors' key is not present
		}

		var module Module
		var moduleParams interface{}
		for k, v := range block {
			if !containsInSlice(arguments, k) {
				if m, ok := GetModule(k); ok {
					task.Module = k
					module = m
					moduleParams = v
					break
				} else {
					errors = append(errors, fmt.Errorf("unknown module or key %q in task %q", k, task.Name))
					errored = true
				}
			}
		}
		if errored {
			continue
		}

		// Skip processing module if we already errored on 'when'
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

		// params is now of type interface{} containing a pointer to the InputType (e.g., **AptInput).
		// We need the value it points to (e.g., *AptInput) to check against the interface.
		paramsPtrValue := reflect.ValueOf(params).Elem()

		// Ensure the pointed-to value is valid before trying to get its interface
		if !paramsPtrValue.IsValid() {
			// This might happen if reflect.New failed, though unlikely here
			errors = append(errors, fmt.Errorf("internal error: invalid pointer created for module %s params", task.Module))
			continue
		}

		// Get the interface{} representation of the pointed-to value (e.g., *AptInput)
		paramsInterface := paramsPtrValue.Interface()

		// Assert the pointed-to value against the ModuleInput interface
		if typedParams, ok := paramsInterface.(ModuleInput); ok {
			task.Params = typedParams // Store the pointer (e.g., *AptInput) that implements the interface
		} else {
			// This error case might indicate a fundamental issue with the module's InputType registration
			// or the interface implementation itself.
			errors = append(errors, fmt.Errorf("params value (%T) does not implement ModuleInput for module %s", paramsInterface, task.Module))
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
