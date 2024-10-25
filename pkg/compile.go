package pkg

import (
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

func Indent(n int) string {
	if n == 0 {
		return ""
	}
	return "\t" + Indent(n-1)
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
	}

	var tasks []Task
	for _, block := range yamlMap {
		task := Task{
			Name:     getStringFromMap(block, "name"),
			Validate: getStringFromMap(block, "validate"),
			Before:   getStringFromMap(block, "before"),
			After:    getStringFromMap(block, "after"),
			When:     getStringFromMap(block, "when"),
			Register: getStringFromMap(block, "register"),
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
					return nil, fmt.Errorf("unknown key %s in task %s", k, task.Name)
				}
			}
		}

		fmt.Printf("Module: %v, params: %v\n", module, moduleParams)

		// Convert back to yaml so we can unmarshal it into the correct type
		paramsData, err := yaml.Marshal(moduleParams)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params for module %s: %v", task.Module, moduleParams)
		}

		// Now we can unmarshal the params into the correct type
		params := reflect.New(module.InputType()).Interface()
		if err := yaml.Unmarshal(paramsData, params); err != nil {
			return nil, fmt.Errorf("failed to unmarshal params for module %s: %v", task.Module, moduleParams)
		}
		task.Params = params.(ModuleInput)

		tasks = append(tasks, task)
	}
	return tasks, nil
}
