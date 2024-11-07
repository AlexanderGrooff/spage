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
	for k, _ := range m {
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

func TextToTasks(text []byte) ([]Task, error) {
	// Unmarshalling the yaml directly into []Task doesn't work because the params field
	// has a dynamic type based on the kind of module that is being used.
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

	var tasks []Task
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

		// Handle include module results
		if task.Module == "include" {
			includeModule, ok := GetModule("include")
			if !ok {
				errors = append(errors, fmt.Errorf("include module not found"))
				continue
			}

			ctx := &HostContext{Facts: make(Facts)} // Empty context for initial include
			// Need to dereference the pointer but avoid importing the concrete type
			paramsVal := reflect.ValueOf(task.Params).Elem().Interface().(ModuleInput)
			output, err := includeModule.Execute(paramsVal, ctx, task.RunAs)
			if err != nil {
				errors = append(errors, fmt.Errorf("failed to include tasks from %v: %v", task.Params, err))
				continue
			}

			o := OutputToFacts(output)
			includedTasks := o["Tasks"].([]Task)
			DebugOutput("Included tasks %v from output %v", includedTasks, output)
			tasks = append(tasks, includedTasks...)
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
