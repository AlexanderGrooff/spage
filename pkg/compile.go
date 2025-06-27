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

// parseBoolOrStringBoolValue parses a value from a map that can be a boolean
// or a string representation of a boolean ("true", "yes", "false", "no").
// It returns the parsed boolean value, a flag indicating if the key was found, and any error.
func parseBoolOrStringBoolValue(block map[string]interface{}, key string, taskName string) (value bool, found bool, err error) {
	rawVal, keyExists := block[key]
	if !keyExists {
		return false, false, nil // Return default false, not found, no error
	}

	switch v := rawVal.(type) {
	case bool:
		return v, true, nil
	case string:
		lowerV := strings.ToLower(v)
		if lowerV == "true" || lowerV == "yes" {
			return true, true, nil
		} else if lowerV == "false" || lowerV == "no" {
			return false, true, nil
		} else {
			// Invalid string value
			err = fmt.Errorf("invalid string value (%q) for '%s' key in task %q, expected 'true'/'yes' or 'false'/'no'", v, key, taskName)
			return false, true, err
		}
	default:
		// Invalid type
		err = fmt.Errorf("invalid type (%T) for '%s' key in task %q, expected boolean or boolean-like string", rawVal, key, taskName)
		return false, true, err
	}
}

// parseConditionString parses a condition field (like 'when' or 'failed_when')
// that accepts either a string condition, a boolean literal, or a list of conditions.
// It returns the condition as an interface{} that can be a string, bool, or []interface{}.
func parseConditionString(block map[string]interface{}, key string, taskName string) (condition interface{}, err error) {
	rawVal, keyExists := block[key]
	if !keyExists {
		return nil, nil // Default nil, no error
	}

	switch v := rawVal.(type) {
	case string:
		return v, nil
	case bool:
		return v, nil
	case []interface{}:
		// Validate that all elements in the list are strings
		for i, item := range v {
			if _, ok := item.(string); !ok {
				return nil, fmt.Errorf("invalid type (%T) for item %d in '%s' list in task %q, expected string", item, i, key, taskName)
			}
		}
		return v, nil
	default:
		// Invalid type
		err = fmt.Errorf("invalid type (%T) for '%s' key in task %q, expected string, boolean, or list of strings", rawVal, key, taskName)
		return nil, err
	}
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
		"become",
		"become_user",
		"ignore_errors",
		"failed_when",
		"changed_when",
		"loop",
		"delegate_to",
		"run_once",
		"no_log",
		"until",
		"retries",
		"delay",
		"tags",
		"check_mode",
		"diff",
		"vars",
	}

	var tasks []GraphNode
	var errors []error

	for idx, block := range blocks {
		task := Task{
			Id:         idx,
			Name:       getStringFromMap(block, "name"),
			Validate:   getStringFromMap(block, "validate"),
			Before:     getStringFromMap(block, "before"),
			After:      getStringFromMap(block, "after"),
			Register:   getStringFromMap(block, "register"),
			RunAs:      getStringFromMap(block, "run_as"),
			DelegateTo: getStringFromMap(block, "delegate_to"),
			Until:      getStringFromMap(block, "until"),
			// Booleans (that might be strings like 'yes') are handled below
		}

		// Declare errored flag here
		var errored bool

		// Handle 'when' using the helper function
		whenCond, whenErr := parseConditionString(block, "when", task.Name)
		if whenErr != nil {
			errors = append(errors, whenErr)
			errored = true
		} else {
			// Convert to string for backwards compatibility (when field is still string)
			if whenCond == nil {
				task.When = ""
			} else if whenStr, ok := whenCond.(string); ok {
				task.When = whenStr
			} else if whenBool, ok := whenCond.(bool); ok {
				task.When = fmt.Sprintf("%t", whenBool)
			} else {
				// For lists, we'd need to update the When field to interface{} too,
				// but for now let's convert the first condition as a temporary solution
				if whenList, ok := whenCond.([]interface{}); ok && len(whenList) > 0 {
					if firstCond, ok := whenList[0].(string); ok {
						task.When = firstCond
					} else {
						task.When = ""
					}
				} else {
					task.When = ""
				}
			}
		}

		becomeUser := getStringFromMap(block, "become_user")

		// Handle 'become' using the helper function
		_, becomeFound, becomeErr := parseBoolOrStringBoolValue(block, "become", task.Name)
		if becomeErr != nil {
			errors = append(errors, becomeErr)
			errored = true
		}

		if task.RunAs != "" && (becomeFound || becomeUser != "") {
			errors = append(errors, fmt.Errorf("'become'/'become_user' and 'run_as' are mutually exclusive"))
			errored = true
		}

		// Use become/become_user to fill in run_as
		if becomeFound && becomeUser != "" {
			task.RunAs = becomeUser
		} else if becomeFound && becomeUser == "" {
			task.RunAs = "root"
		}

		// Handle 'ignore_errors' using the helper function
		ignoreVal, ignoreFound, ignoreErr := parseBoolOrStringBoolValue(block, "ignore_errors", task.Name)
		if ignoreErr != nil {
			errors = append(errors, ignoreErr)
			errored = true
		} else {
			// If found, use the parsed value, otherwise default to false (handled by initial Task struct value)
			if ignoreFound {
				task.IgnoreErrors = ignoreVal
			} // else task.IgnoreErrors keeps its default zero value (false)
		}

		// Handle 'no_log' using the helper function
		noLogVal, noLogFound, noLogErr := parseBoolOrStringBoolValue(block, "no_log", task.Name)
		if noLogErr != nil {
			errors = append(errors, noLogErr)
			errored = true
		} else {
			// If found, use the parsed value, otherwise default to false (handled by initial Task struct value)
			if noLogFound {
				task.NoLog = noLogVal
			} // else task.RunOnce keeps its default zero value (false)
		}

		// Handle 'run_once' using the helper function
		runOnceVal, runOnceFound, runOnceErr := parseBoolOrStringBoolValue(block, "run_once", task.Name)
		if runOnceErr != nil {
			errors = append(errors, runOnceErr)
			errored = true
		} else {
			// If found, use the parsed value, otherwise default to false (handled by initial Task struct value)
			if runOnceFound {
				task.RunOnce = runOnceVal
			} // else task.RunOnce keeps its default zero value (false)
		}

		// Handle 'check_mode' using the helper function
		checkModeVal, checkModeFound, checkModeErr := parseBoolOrStringBoolValue(block, "check_mode", task.Name)
		if checkModeErr != nil {
			errors = append(errors, checkModeErr)
			errored = true
		} else {
			if checkModeFound {
				task.CheckMode = &checkModeVal
			}
		}

		// Handle 'check_mode' using the helper function
		diffVal, diffFound, diffErr := parseBoolOrStringBoolValue(block, "diff", task.Name)
		if diffErr != nil {
			errors = append(errors, diffErr)
			errored = true
		} else {
			if diffFound {
				task.Diff = &diffVal
			}
		}

		if retriesVal, ok := block["retries"]; ok {
			if v, ok := retriesVal.(int); ok {
				task.Retries = v
			} else {
				errors = append(errors, fmt.Errorf("invalid type (%T) for 'retries' key in task %q, expected integer", retriesVal, task.Name))
				errored = true
			}
		}

		if delayVal, ok := block["delay"]; ok {
			if v, ok := delayVal.(int); ok {
				task.Delay = v
			} else {
				errors = append(errors, fmt.Errorf("invalid type (%T) for 'delay' key in task %q, expected integer", delayVal, task.Name))
				errored = true
			}
		}

		// Handle 'tags' field - can be a string or list of strings
		if tagsVal, ok := block["tags"]; ok {
			switch v := tagsVal.(type) {
			case string:
				task.Tags = []string{v}
			case []interface{}:
				for i, tagVal := range v {
					if tagStr, ok := tagVal.(string); ok {
						task.Tags = append(task.Tags, tagStr)
					} else {
						errors = append(errors, fmt.Errorf("invalid type (%T) for item %d in 'tags' list in task %q, expected string", tagVal, i, task.Name))
						errored = true
						break
					}
				}
			default:
				errors = append(errors, fmt.Errorf("invalid type (%T) for 'tags' key in task %q, expected string or list of strings", tagsVal, task.Name))
				errored = true
			}
		}

		// Handle 'failed_when' using the helper function
		failedWhenCond, failedWhenErr := parseConditionString(block, "failed_when", task.Name)
		if failedWhenErr != nil {
			errors = append(errors, failedWhenErr)
			errored = true
		} else {
			task.FailedWhen = failedWhenCond
		}

		changedWhenCond, changedWhenErr := parseConditionString(block, "changed_when", task.Name)
		if changedWhenErr != nil {
			errors = append(errors, changedWhenErr)
			errored = true
		} else {
			task.ChangedWhen = changedWhenCond
		}

		// Handle 'vars' field - can be a map of variables
		if varsVal, ok := block["vars"]; ok {
			// vars can be a map[string]interface{} or other types
			task.Vars = varsVal
		}

		var module Module
		var moduleParams interface{}
		for k, v := range block {
			if !containsInSlice(arguments, k) {
				if task.Module != "" {
					errors = append(errors, fmt.Errorf("multiple module keys found ('%s' and '%s') in task %q", task.Module, k, task.Name))
					errored = true
					break
				}
				if m, ok := GetModule(k); ok {
					task.Module = k
					module = m
					moduleParams = v
				} else {
					// Handle unknown modules with Python fallback
					task.Module = "ansible_python" // Use the Python fallback module name
					if pythonModule, ok := GetModule("ansible_python"); ok {
						module = pythonModule

						// Convert rawParams to map[string]interface{}
						var paramsMap map[string]interface{}
						if v != nil {
							if pm, ok := v.(map[string]interface{}); ok {
								paramsMap = pm
							} else {
								// Try to convert other types to a simple parameter
								paramsMap = map[string]interface{}{"value": v}
							}
						} else {
							paramsMap = make(map[string]interface{})
						}

						// Create the AnsiblePythonInput structure
						moduleParams = map[string]interface{}{
							"module_name": k,
							"args":        paramsMap,
						}
					} else {
						errors = append(errors, fmt.Errorf("ansible_python module not registered for unknown module %s", k))
						errored = true
						break
					}
				}
			}
		}

		if !errored && task.Module == "" {
			errors = append(errors, fmt.Errorf("no module specified for task %q", task.Name))
			errored = true
		}

		if errored {
			continue
		}

		if loopVal, ok := block["loop"]; ok {
			task.Loop = loopVal
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

		// Assert the pointed-to value against the ConcreteModuleInputProvider interface
		if typedParams, ok := paramsInterface.(ConcreteModuleInputProvider); ok {
			task.Params.Actual = typedParams // Store the provider in task.Params.Actual
		} else {
			// This error case might indicate a fundamental issue with the module's InputType registration
			// or the interface implementation itself.
			errors = append(errors, fmt.Errorf("params value (%T) does not implement ConcreteModuleInputProvider for module %s", paramsInterface, task.Module))
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
		Tasks:          make([][]Task, len(graph.Tasks)),
	}

	// Replace variables in each task with host-specific values
	for i, taskLayer := range graph.Tasks {
		compiledGraph.Tasks[i] = make([]Task, len(taskLayer))
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
func compileNode(node GraphNode, host Host) (Task, error) {
	switch n := node.(type) {
	case Task:
		task := n
		v := reflect.ValueOf(&task).Elem()

		closure := TempClosureForHost(&host)

		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.Kind() == reflect.String {
				strVal := field.String()
				// Pass the converted *sync.Map to TemplateString
				if templated, err := TemplateString(strVal, closure); err == nil {
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
	}
	return Task{}, fmt.Errorf("unknown node type: %T", node)
}
