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
		switch lowerV {
		case "true", "yes":
			return true, true, nil
		case "false", "no":
			return false, true, nil
		default:
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

func parseJinjaExpression(block map[string]interface{}, key string, taskName string) (JinjaExpression, error) {
	rawVal, keyExists := block[key]
	if !keyExists {
		return JinjaExpression{}, nil // Default nil, no error
	}

	switch v := rawVal.(type) {
	case string:
		return JinjaExpression{Expression: v}, nil
	case bool:
		return JinjaExpression{Expression: fmt.Sprintf("%v", v)}, nil
	default:
		return JinjaExpression{}, fmt.Errorf("invalid type (%T) for '%s' key in task %q, expected string or bool", rawVal, key, taskName)
	}
}

func parseJinjaExpressionList(block map[string]interface{}, key string, taskName string) (JinjaExpressionList, error) {
	rawVal, keyExists := block[key]
	if !keyExists {
		return nil, nil // Default nil, no error
	}

	switch v := rawVal.(type) {
	case string:
		return JinjaExpressionList{JinjaExpression{Expression: v}}, nil
	case bool:
		return JinjaExpressionList{JinjaExpression{Expression: fmt.Sprintf("%v", v)}}, nil
	case []interface{}:
		result := make(JinjaExpressionList, len(v))
		for i, item := range v {
			switch itemTyped := item.(type) {
			case string:
				result[i] = JinjaExpression{Expression: itemTyped}
			case bool:
				result[i] = JinjaExpression{Expression: fmt.Sprintf("%v", itemTyped)}
			default:
				return nil, fmt.Errorf("invalid type (%T) for item %d in '%s' list in task %q, expected string or bool", item, i, key, taskName)
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("invalid type (%T) for '%s' key in task %q, expected string, bool, or list of them", rawVal, key, taskName)
	}
}

func isRootBlock(block map[string]interface{}) bool {
	return block["is_root"] == true
}

func isHandlerBlock(block map[string]interface{}) bool {
	return block["is_handler"] == true
}

func isRoleDefaultsBlock(block map[string]interface{}) bool {
	return block["is_role_defaults"] == true
}

func isRoleVarsBlock(block map[string]interface{}) bool {
	return block["is_role_vars"] == true
}

func ParsePlayAttributes(blocks []map[string]interface{}) (map[string]interface{}, error) {
	// Find the root block (can be anywhere in the list)
	var rootBlock map[string]interface{}
	var found bool
	for _, block := range blocks {
		if isRootBlock(block) {
			rootBlock = block
			found = true
			break
		}
	}

	if !found {
		// Create a synthetic root block for tasks-only playbooks
		rootBlock = map[string]interface{}{}
	}

	attributes := make(map[string]interface{})

	// Initialize vars from root block
	vars := make(map[string]interface{})
	if rootVars, ok := rootBlock["vars"].(map[string]interface{}); ok {
		for k, v := range rootVars {
			vars[k] = v
		}
	}

	// Merge role defaults first (lowest precedence)
	for _, block := range blocks {
		if isRoleDefaultsBlock(block) {
			if roleVars, ok := block["vars"].(map[string]interface{}); ok {
				for k, v := range roleVars {
					// Only set if not already defined (defaults have lowest precedence)
					if _, exists := vars[k]; !exists {
						vars[k] = v
					}
				}
			}
		}
	}

	// Merge role vars (higher precedence than defaults, lower than play vars)
	for _, block := range blocks {
		if isRoleVarsBlock(block) {
			if roleVars, ok := block["vars"].(map[string]interface{}); ok {
				for k, v := range roleVars {
					// Role vars override defaults but not play vars
					if _, exists := vars[k]; !exists {
						vars[k] = v
					} else {
						// Check if the existing var came from defaults or root
						// For simplicity, role vars will override defaults
						vars[k] = v
					}
				}
			}
		}
	}

	attributes["vars"] = vars
	return attributes, nil
}

// parseShorthandParams parses Ansible shorthand parameter syntax like "src=file.j2 dest=/path/file"
// and converts it to a map[string]interface{} structure.
// Returns the converted map if input is shorthand, nil if input is already correct format, or error if parsing fails.
func parseShorthandParams(moduleParams interface{}, moduleName, taskName string) (map[string]interface{}, error) {
	// Only process if moduleParams is a string (shorthand syntax)
	paramStr, isString := moduleParams.(string)
	if !isString {
		return nil, nil // Not shorthand, return nil to indicate no conversion needed
	}

	// Don't attempt shorthand parsing for modules that typically use raw commands
	if moduleName == "shell" || moduleName == "ansible.builtin.shell" ||
		moduleName == "command" || moduleName == "ansible.builtin.command" {
		return nil, nil // Let the module handle its own parsing
	}

	// Only attempt to parse as key=value pairs if the string contains '=' characters
	// This prevents modules that use plain string syntax from being incorrectly parsed
	if !strings.Contains(paramStr, "=") {
		return nil, nil // Not key=value shorthand, let the module handle it
	}

	// Parse the key=value pairs
	result := make(map[string]interface{})

	// Split by spaces, but handle quoted values that may contain spaces
	pairs, err := parseKeyValuePairs(paramStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse shorthand parameters for module %s in task %q: %w", moduleName, taskName, err)
	}

	for key, value := range pairs {
		result[key] = value
	}

	// If no key=value pairs were found, this might not be shorthand syntax
	if len(result) == 0 {
		return nil, fmt.Errorf("no key=value pairs found in parameter string %q for module %s in task %q", paramStr, moduleName, taskName)
	}

	return result, nil
}

// parseKeyValuePairs parses a string like "src=file.j2 dest=/path/file mode=0644"
// and returns a map of key-value pairs. Handles quoted values.
func parseKeyValuePairs(input string) (map[string]string, error) {
	result := make(map[string]string)

	// Trim whitespace
	input = strings.TrimSpace(input)
	if input == "" {
		return result, nil
	}

	// Use a simple state machine to parse key=value pairs
	var currentKey, currentValue strings.Builder
	var inValue, inQuotes bool
	var quoteChar rune

	i := 0
	for i < len(input) {
		char := rune(input[i])

		switch {
		case !inValue && char == '=':
			// Found the = separator
			inValue = true

		case !inValue:
			// Building the key
			if char != ' ' && char != '\t' {
				currentKey.WriteRune(char)
			}

		case inValue && !inQuotes && (char == '"' || char == '\''):
			// Starting a quoted value
			inQuotes = true
			quoteChar = char

		case inValue && inQuotes && char == quoteChar:
			// Ending a quoted value
			inQuotes = false

		case inValue && !inQuotes && (char == ' ' || char == '\t'):
			// End of this key=value pair
			key := strings.TrimSpace(currentKey.String())
			value := strings.TrimSpace(currentValue.String())

			if key == "" {
				return nil, fmt.Errorf("empty key found in parameter string")
			}

			result[key] = value

			// Reset for next pair
			currentKey.Reset()
			currentValue.Reset()
			inValue = false

		case inValue:
			// Building the value
			currentValue.WriteRune(char)
		}

		i++
	}

	// Handle the last key=value pair
	if currentKey.Len() > 0 {
		key := strings.TrimSpace(currentKey.String())
		value := strings.TrimSpace(currentValue.String())

		if key == "" {
			return nil, fmt.Errorf("empty key found in parameter string")
		}

		if inValue {
			result[key] = value
		} else {
			// Key without value might be a boolean flag
			return nil, fmt.Errorf("key %q found without value in parameter string", key)
		}
	}

	return result, nil
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
		"no_log", // Caught but ignored
		"until",
		"retries",
		"delay",
		"tags",
		"notify",
		"check_mode",
		"diff",
		"vars",
		"is_handler",
		"_role_name",
		"_role_path",
		"with_items",
		"throttle", // Caught but ignored
		"local_action",
	}

	var tasks []GraphNode
	var errors []error

	for idx, block := range blocks {
		if isRootBlock(block) || isRoleDefaultsBlock(block) || isRoleVarsBlock(block) {
			continue
		}

		task := Task{
			Id:         idx,
			Name:       getStringFromMap(block, "name"),
			Validate:   getStringFromMap(block, "validate"),
			Before:     getStringFromMap(block, "before"),
			After:      getStringFromMap(block, "after"),
			Register:   getStringFromMap(block, "register"),
			DelegateTo: getStringFromMap(block, "delegate_to"),
			IsHandler:  isHandlerBlock(block),
			RoleName:   getStringFromMap(block, "_role_name"),
			RolePath:   getStringFromMap(block, "_role_path"),
			// Booleans (that might be strings like 'yes') are handled below
		}

		// Declare errored flag here
		var errored bool

		until, untilErr := parseJinjaExpression(block, "until", task.Name)
		if untilErr != nil {
			errors = append(errors, untilErr)
			errored = true
		} else {
			task.Until = until
		}

		// Handle 'when' using the helper function
		whenCond, whenErr := parseJinjaExpressionList(block, "when", task.Name)
		if whenErr != nil {
			errors = append(errors, whenErr)
			errored = true
		} else {
			task.When = whenCond
		}

		task.BecomeUser = getStringFromMap(block, "run_as")
		if task.BecomeUser != "" {
			task.Become = true
		}

		becomeUser := getStringFromMap(block, "become_user")

		// Handle 'become' using the helper function
		_, become, becomeErr := parseBoolOrStringBoolValue(block, "become", task.Name)
		if becomeErr != nil {
			errors = append(errors, becomeErr)
			errored = true
		}
		task.Become = become

		if task.BecomeUser != "" && (become || becomeUser != "") {
			errors = append(errors, fmt.Errorf("'become'/'become_user' and 'run_as' are mutually exclusive"))
			errored = true
		}

		// Use become/become_user to fill in run_as
		if become && becomeUser != "" {
			task.BecomeUser = becomeUser
		} else if become {
			task.BecomeUser = "root"
		}

		// Handle 'ignore_errors' using the helper function
		ignoreErrors, ignoreErrorsErr := parseJinjaExpression(block, "ignore_errors", task.Name)
		if ignoreErrorsErr != nil {
			errors = append(errors, ignoreErrorsErr)
			errored = true
		} else {
			task.IgnoreErrors = ignoreErrors
		}

		// Handle 'no_log' using the helper function
		noLog, noLogErr := parseJinjaExpression(block, "no_log", task.Name)
		if noLogErr != nil {
			errors = append(errors, noLogErr)
			errored = true
		} else {
			// If found, use the parsed value, otherwise default to false (handled by initial Task struct value)
			task.NoLog = noLog
		}

		// Handle 'run_once' using the helper function
		runOnce, runOnceErr := parseJinjaExpression(block, "run_once", task.Name)
		if runOnceErr != nil {
			errors = append(errors, runOnceErr)
			errored = true
		} else {
			// If found, use the parsed value, otherwise default to false (handled by initial Task struct value)
			task.RunOnce = runOnce
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

		// Handle 'notify' field - can be a string or list of strings
		if notifyVal, ok := block["notify"]; ok {
			switch v := notifyVal.(type) {
			case string:
				task.Notify = []string{v}
			case []interface{}:
				for i, handlerVal := range v {
					if handlerStr, ok := handlerVal.(string); ok {
						task.Notify = append(task.Notify, handlerStr)
					} else {
						errors = append(errors, fmt.Errorf("invalid type (%T) for item %d in 'notify' list in task %q, expected string", handlerVal, i, task.Name))
						errored = true
						break
					}
				}
			default:
				errors = append(errors, fmt.Errorf("invalid type (%T) for 'notify' key in task %q, expected string or list of strings", notifyVal, task.Name))
				errored = true
			}
		}

		// Handle 'failed_when' using the helper function
		failedWhen, failedWhenErr := parseJinjaExpressionList(block, "failed_when", task.Name)
		if failedWhenErr != nil {
			errors = append(errors, failedWhenErr)
			errored = true
		} else {
			task.FailedWhen = failedWhen
		}

		changedWhen, changedWhenErr := parseJinjaExpressionList(block, "changed_when", task.Name)
		if changedWhenErr != nil {
			errors = append(errors, changedWhenErr)
			errored = true
		} else {
			task.ChangedWhen = changedWhen
		}

		// Handle 'vars' field - can be a map of variables
		if varsVal, ok := block["vars"]; ok {
			// vars can be a map[string]interface{} or other types
			task.Vars = varsVal
		}

		var moduleName string
		var moduleParams interface{}

		// Handle local_action by transforming it into a normal task with delegate_to: localhost
		if localAction, ok := block["local_action"]; ok {
			task.DelegateTo = "localhost"

			switch v := localAction.(type) {
			case string:
				parts := strings.Fields(v)
				if len(parts) > 0 {
					moduleName = parts[0]
					moduleParams = strings.Join(parts[1:], " ")
				} else {
					errors = append(errors, fmt.Errorf("invalid local_action format in task %q: empty string", task.Name))
					errored = true
				}
			case map[string]interface{}:
				if mod, exists := v["module"]; exists {
					moduleName = mod.(string)
					// The rest of the map is the parameters
					delete(v, "module")
					moduleParams = v
				} else {
					errors = append(errors, fmt.Errorf("invalid local_action format in task %q: 'module' key not found", task.Name))
					errored = true
				}
			default:
				errors = append(errors, fmt.Errorf("invalid type for local_action in task %q: got %T", localAction, localAction))
				errored = true
			}
		} else {
			// Default behavior: find the module as a key that is not a standard argument
			for key, value := range block {
				if !containsInSlice(arguments, key) {
					moduleName = key
					moduleParams = value
					break
				}
			}
		}

		// If a module was identified, proceed with processing
		if moduleName != "" {
			task.Module = moduleName
		} else if !errored {
			// Only error if no other error has occurred for this task
			errors = append(errors, fmt.Errorf("no module specified for task %q", task.Name))
			errored = true
		}

		var module Module
		if m, ok := GetModule(task.Module); ok {
			module = m
		} else {
			// Handle unknown modules with Python fallback
			task.Module = "ansible_python" // Use the Python fallback module name
			if pythonModule, ok := GetModule("ansible_python"); ok {
				module = pythonModule

				// Convert rawParams to map[string]interface{}
				var paramsMap map[string]interface{}
				if moduleParams != nil {
					if pm, ok := moduleParams.(map[string]interface{}); ok {
						paramsMap = pm
					} else {
						// Try to convert other types to a simple parameter
						paramsMap = map[string]interface{}{"value": moduleParams}
					}
				} else {
					paramsMap = make(map[string]interface{})
				}

				// Create the AnsiblePythonInput structure
				moduleParams = map[string]interface{}{
					"module_name": moduleName,
					"args":        paramsMap,
				}
			} else {
				errors = append(errors, fmt.Errorf("ansible_python module not registered for unknown module %s", moduleName))
				errored = true
			}
		}

		if !errored && task.Module == "" {
			errors = append(errors, fmt.Errorf("no module specified for task %q", task.Name))
			errored = true
		}

		if errored {
			continue
		}

		if withItemsVal, ok := block["with_items"]; ok {
			task.Loop = withItemsVal
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

		// *** Shorthand Parameter Parsing Start ***
		// Handle Ansible shorthand syntax like "template: src=file.j2 dest=/path/file"
		if shorthandParams, err := parseShorthandParams(moduleParams, task.Module, task.Name); err != nil {
			errors = append(errors, err)
			continue
		} else if shorthandParams != nil {
			moduleParams = shorthandParams
		}
		// *** Shorthand Parameter Parsing End ***

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

// Helper function to convert map[string]interface{} to *sync.Map
func MapToSyncMap(m map[string]interface{}) *sync.Map {
	sm := new(sync.Map)
	for k, v := range m {
		sm.Store(k, v)
	}
	return sm
}
