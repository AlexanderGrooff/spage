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

func getStringFromMap(m map[string]interface{}, key string) string {
	if value, ok := m[key]; ok {
		return value.(string)
	}
	return ""
}

func TextToTasks(text []byte) ([]Task, error) {
	var yamlMap []map[string]interface{}
	err := yaml.Unmarshal([]byte(text), &yamlMap)
	if err != nil {
		return nil, err
	}

	var tasks []Task
	for _, block := range yamlMap {
		m := block["module"]
		module, ok := GetModule(m.(string))
		if !ok {
			return nil, fmt.Errorf("module %s not found", m)
		}
		fmt.Printf("Module: %v, params: %v\n", module, block["params"])

		// Convert back to yaml so we can unmarshal it into the correct type
		paramsData, err := yaml.Marshal(block["params"])
		if err != nil {
			return nil, fmt.Errorf("Failed to marshal params for module %s: %v", m, block["params"])
		}

		// Now we can unmarshal the params into the correct type
		params := reflect.New(module.InputType()).Interface()
		if err := yaml.Unmarshal(paramsData, params); err != nil {
			return nil, fmt.Errorf("Failed to unmarshal params for module %s: %v", m, block["params"])
		}

		tasks = append(tasks, Task{
			Name:     getStringFromMap(block, "name"),
			Module:   m.(string),
			Params:   params.(ModuleInput),
			Validate: getStringFromMap(block, "validate"),
			Before:   getStringFromMap(block, "before"),
			After:    getStringFromMap(block, "after"),
			When:     getStringFromMap(block, "when"),
			Register: getStringFromMap(block, "register"),
		})
	}
	return tasks, nil
}
