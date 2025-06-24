package pkg

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// ModuleInput is a marker interface for module input parameters.
// It ensures that module parameters have a method to generate their code representation.
type ConcreteModuleInputProvider interface {
	ToCode() string
	GetVariableUsage() []string
	Validate() error
	// HasRevert indicates if this input defines a revert action.
	HasRevert() bool
	// ProvidesVariables returns a list of variable names this input might define (e.g., keys in set_fact).
	ProvidesVariables() []string
}

// Its primary role is to facilitate correct JSON/YAML marshaling/unmarshaling
// when it's a field within another struct (like Task).
type ModuleInput struct {
	// Actual holds the concrete *ShellInput, *CopyInput, etc.
	// Let the standard marshalers handle this field when marshaling the parent Task struct,
	// using the custom Task MarshalJSON.
	Actual ConcreteModuleInputProvider `json:"actual,omitempty" yaml:"actual,omitempty"`
}

// ToCode delegates to the Actual ConcreteModuleInputProvider.
// It's called during code generation, Actual must be populated.
func (mi *ModuleInput) ToCode() string {
	if mi.Actual == nil {
		// This indicates a programming error if called before Actual is populated.
		panic("ModuleInput.Actual is nil when ToCode() is called")
	}
	return mi.Actual.ToCode()
}

// GetVariableUsage delegates to the Actual ConcreteModuleInputProvider.
func (mi *ModuleInput) GetVariableUsage() []string {
	if mi.Actual == nil {
		panic("ModuleInput.Actual is nil when GetVariableUsage() is called")
	}
	return mi.Actual.GetVariableUsage()
}

// Validate delegates to the Actual ConcreteModuleInputProvider.
func (mi *ModuleInput) Validate() error {
	if mi.Actual == nil {
		panic("ModuleInput.Actual is nil when Validate() is called")
	}
	return mi.Actual.Validate()
}

// HasRevert delegates to the Actual ConcreteModuleInputProvider.
func (mi *ModuleInput) HasRevert() bool {
	if mi.Actual == nil {
		// Default to false if Actual is not populated, or panic as above.
		// For safety, returning false might be less disruptive than panicking here.
		return false
	}
	return mi.Actual.HasRevert()
}

// ProvidesVariables delegates to the Actual ConcreteModuleInputProvider.
func (mi *ModuleInput) ProvidesVariables() []string {
	if mi.Actual == nil {
		// Default to nil or empty slice.
		return nil
	}
	return mi.Actual.ProvidesVariables()
}

// ModuleOutput is a marker interface for module output results.
// It ensures that module results can indicate whether they represent a change.
type ModuleOutput interface {
	Changed() bool
	String() string
}

type RevertableChange[T comparable] struct {
	Before T
	After  T
}

func (r RevertableChange[T]) Changed() bool {
	return r.Before != r.After
}

type Module interface {
	InputType() reflect.Type
	OutputType() reflect.Type
	Execute(params ConcreteModuleInputProvider, c *Closure, runAs string) (ModuleOutput, error)
	Revert(params ConcreteModuleInputProvider, c *Closure, previous ModuleOutput, runAs string) (ModuleOutput, error)
	ParameterAliases() map[string]string
}

var registeredModules = make(map[string]Module)

// RegisterModule allows modules to register themselves by name.
func RegisterModule(name string, module Module) {
	if _, exists := registeredModules[name]; exists {
		panic(fmt.Sprintf("Module %s already registered", name))
	}
	registeredModules[name] = module
}

// GetModule retrieves a registered module by name.
func GetModule(name string) (Module, bool) {
	module, ok := registeredModules[name]
	return module, ok
}

// GetPythonFallbackModule creates a Python fallback module for unknown modules at runtime
func GetPythonFallbackModule(moduleName string, originalParams ConcreteModuleInputProvider) (Module, ConcreteModuleInputProvider, error) {
	// Import this dynamically at runtime to avoid import cycles
	// We'll use reflection to call the function from the modules package
	// For now, create a basic implementation inline

	// Create a simple wrapper that implements the Module interface
	pythonModule := &runtimePythonFallback{ModuleName: moduleName}

	// Create the input parameters
	paramsMap := make(map[string]interface{})
	if originalParams != nil {
		var err error
		paramsMap, err = convertStructToMap(originalParams)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to convert module params for Python fallback: %w", err)
		}
	}

	pythonInput := &runtimePythonInput{
		ModuleName: moduleName,
		Args:       paramsMap,
	}

	return pythonModule, pythonInput, nil
}

// convertStructToMap converts a struct to a map[string]interface{} using reflection
func convertStructToMap(obj interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	if obj == nil {
		return result, nil
	}

	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %T", obj)
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		// Skip unexported fields
		if !fieldValue.CanInterface() {
			continue
		}

		// Get the YAML or JSON tag name, or use the field name
		tagName := field.Name
		if yamlTag := field.Tag.Get("yaml"); yamlTag != "" && yamlTag != "-" {
			tagName = strings.Split(yamlTag, ",")[0]
		} else if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
			tagName = strings.Split(jsonTag, ",")[0]
		}

		// Skip fields that shouldn't be included
		if tagName == "-" || tagName == "" {
			continue
		}

		result[tagName] = fieldValue.Interface()
	}

	return result, nil
}

// Runtime types to avoid import cycle
type runtimePythonInput struct {
	ModuleName string                 `yaml:"module_name" json:"module_name"`
	Args       map[string]interface{} `yaml:"args" json:"args"`
}

func (i *runtimePythonInput) ToCode() string {
	// Convert Args map to Go code format
	argsCode := "map[string]interface{}{"
	for k, v := range i.Args {
		switch val := v.(type) {
		case string:
			argsCode += fmt.Sprintf("%q:%q,", k, val)
		case bool:
			argsCode += fmt.Sprintf("%q:%t,", k, val)
		case int, int32, int64:
			argsCode += fmt.Sprintf("%q:%v,", k, val)
		case float32, float64:
			argsCode += fmt.Sprintf("%q:%v,", k, val)
		case []interface{}:
			// Handle slice values like ["hostname test-switch","interface Ethernet1","  no shutdown"]
			sliceCode := "[]interface{}{"
			for _, item := range val {
				switch itemVal := item.(type) {
				case string:
					sliceCode += fmt.Sprintf("%q,", itemVal)
				default:
					sliceCode += fmt.Sprintf("%v,", itemVal)
				}
			}
			sliceCode += "}"
			argsCode += fmt.Sprintf("%q:%s,", k, sliceCode)
		default:
			argsCode += fmt.Sprintf("%q:interface{}(%v),", k, val)
		}
	}
	argsCode += "}"
	return fmt.Sprintf("modules.AnsiblePythonInput{ModuleName: %q, Args: %s}", i.ModuleName, argsCode)
}

func (i *runtimePythonInput) GetVariableUsage() []string {
	return nil // Basic implementation
}

func (i *runtimePythonInput) Validate() error {
	if i.ModuleName == "" {
		return fmt.Errorf("module_name is required for ansible_python module")
	}
	return nil
}

func (i *runtimePythonInput) HasRevert() bool {
	return false // Python modules don't support revert
}

func (i *runtimePythonInput) ProvidesVariables() []string {
	return nil // We don't know what variables a Python module might provide
}

type runtimePythonOutput struct {
	WasChanged bool                   `yaml:"changed" json:"changed"`
	Failed     bool                   `yaml:"failed" json:"failed"`
	Msg        string                 `yaml:"msg" json:"msg"`
	Results    map[string]interface{} `yaml:"results" json:"results"`
}

func (o runtimePythonOutput) Changed() bool {
	return o.WasChanged
}

func (o runtimePythonOutput) String() string {
	status := "ok"
	if o.Failed {
		status = "failed"
	} else if o.WasChanged {
		status = "changed"
	}
	return fmt.Sprintf("  status: %s\n  msg: %q\n  changed: %v\n", status, o.Msg, o.WasChanged)
}

type runtimePythonFallback struct {
	ModuleName string
}

func (m *runtimePythonFallback) InputType() reflect.Type {
	return reflect.TypeOf((*runtimePythonInput)(nil))
}

func (m *runtimePythonFallback) OutputType() reflect.Type {
	return reflect.TypeOf(runtimePythonOutput{})
}

func (m *runtimePythonFallback) Execute(params ConcreteModuleInputProvider, closure *Closure, runAs string) (ModuleOutput, error) {
	pythonInput, ok := params.(*runtimePythonInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type for Python fallback module: %T", params)
	}

	return m.executePythonModule(*pythonInput, closure, runAs)
}

func (m *runtimePythonFallback) executePythonModule(params runtimePythonInput, closure *Closure, runAs string) (ModuleOutput, error) {
	// Create a temporary directory for the Ansible execution
	tempDir, err := os.MkdirTemp("", "spage-ansible-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Template the arguments with current facts
	templatedArgs := make(map[string]interface{})
	for k, v := range params.Args {
		if strVal, ok := v.(string); ok {
			templated, err := TemplateString(strVal, closure)
			if err != nil {
				return nil, fmt.Errorf("failed to template argument %s: %w", k, err)
			}
			templatedArgs[k] = templated
		} else {
			templatedArgs[k] = v
		}
	}

	// Create the ansible-playbook YAML file
	playbook := map[string]interface{}{
		"hosts":        "localhost",
		"gather_facts": false,
		"connection":   "local",
		"tasks": []map[string]interface{}{
			{
				"name":            fmt.Sprintf("Execute %s", params.ModuleName),
				params.ModuleName: templatedArgs,
			},
		},
	}

	playbookYAML, err := yaml.Marshal([]interface{}{playbook})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal playbook: %w", err)
	}

	playbookFile := filepath.Join(tempDir, "playbook.yml")
	if err := os.WriteFile(playbookFile, playbookYAML, 0644); err != nil {
		return nil, fmt.Errorf("failed to write playbook file: %w", err)
	}

	// Create inventory file
	inventoryContent := "[local]\nlocalhost ansible_connection=local\n"
	inventoryFile := filepath.Join(tempDir, "inventory")
	if err := os.WriteFile(inventoryFile, []byte(inventoryContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write inventory file: %w", err)
	}

	// Execute ansible-playbook
	cmd := exec.Command("ansible-playbook",
		"-i", inventoryFile,
		"-v", // Verbose output to get JSON results
		playbookFile,
	)

	// Set environment variables
	cmd.Env = os.Environ()

	output, err := cmd.CombinedOutput()

	result := m.parseAnsibleOutput(string(output), params.ModuleName)

	if err != nil {
		// Even if ansible-playbook fails, we might get useful output
		if result.Msg == "" {
			result.Msg = fmt.Sprintf("ansible-playbook execution failed: %v", err)
		}
		result.Failed = true

		// If the module truly doesn't exist, return an error instead of just marking as failed
		if strings.Contains(string(output), "couldn't resolve module/action") {
			return nil, fmt.Errorf("module '%s' not found in Ansible installation. This module may require additional Ansible collections or may not exist", params.ModuleName)
		}

		// For other execution failures, still return an error
		return nil, fmt.Errorf("failed to execute module '%s' via Python fallback: %s", params.ModuleName, result.Msg)
	}

	// If execution succeeded but module wasn't found, still return error
	if result.Failed && strings.Contains(result.Msg, "not found in Ansible installation") {
		return nil, fmt.Errorf("module '%s' not found in Ansible installation", params.ModuleName)
	}

	return result, nil
}

func (m *runtimePythonFallback) parseAnsibleOutput(output, moduleName string) runtimePythonOutput {
	result := runtimePythonOutput{
		Results: make(map[string]interface{}),
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Look for JSON output in ansible verbose mode
		if strings.Contains(line, "=>") && strings.Contains(line, "{") {
			jsonStart := strings.Index(line, "{")
			if jsonStart != -1 {
				jsonStr := line[jsonStart:]
				var moduleResult map[string]interface{}
				if err := json.Unmarshal([]byte(jsonStr), &moduleResult); err == nil {
					result.Results = moduleResult

					if changed, ok := moduleResult["changed"].(bool); ok {
						result.WasChanged = changed
					}
					if failed, ok := moduleResult["failed"].(bool); ok {
						result.Failed = failed
					}
					if msg, ok := moduleResult["msg"].(string); ok {
						result.Msg = msg
					}
					break
				}
			}
		}

		// Look for module not found errors
		if strings.Contains(line, "couldn't resolve module/action") {
			result.Failed = true
			result.Msg = fmt.Sprintf("Module '%s' not found in Ansible installation. This module may require additional Ansible collections or may not exist.", moduleName)
			result.Results["ansible_error"] = "module_not_found"
			break
		}

		// Look for general failure indicators
		if strings.Contains(line, "FAILED!") {
			result.Failed = true
			if result.Msg == "" {
				result.Msg = "Ansible execution failed"
			}
		}

		// Look for ERROR! indicators
		if strings.Contains(line, "ERROR!") {
			result.Failed = true
			if result.Msg == "" {
				result.Msg = fmt.Sprintf("Ansible error: %s", line)
			}
		}
	}

	// If we didn't find any structured output, use the raw output
	if len(result.Results) == 0 {
		result.Results["raw_output"] = output
		if result.Msg == "" {
			result.Msg = fmt.Sprintf("Executed %s via ansible-playbook", moduleName)
		}
	}

	return result
}

func (m *runtimePythonFallback) Revert(params ConcreteModuleInputProvider, closure *Closure, previous ModuleOutput, runAs string) (ModuleOutput, error) {
	return runtimePythonOutput{
		Failed: false,
		Msg:    "Python fallback revert (noop)",
	}, nil
}

func (m *runtimePythonFallback) ParameterAliases() map[string]string {
	return nil
}

// GetPythonFallbackForCompilation creates a Python fallback module and params for compilation phase
// This function is used during compilation to create Python fallback modules for unknown modules
func GetPythonFallbackForCompilation(moduleName string, rawParams interface{}) (Module, interface{}) {
	// Import the AnsiblePythonInput type from the modules package
	// We need to create the structure directly to avoid circular imports

	// Convert rawParams to map[string]interface{}
	var paramsMap map[string]interface{}
	if rawParams != nil {
		if pm, ok := rawParams.(map[string]interface{}); ok {
			paramsMap = pm
		} else {
			// Try to convert other types to a simple parameter
			paramsMap = map[string]interface{}{"value": rawParams}
		}
	} else {
		paramsMap = make(map[string]interface{})
	}

	// Create the python input directly as a map that will be marshaled into AnsiblePythonInput
	pythonInput := map[string]interface{}{
		"module_name": moduleName,
		"args":        paramsMap,
	}

	// Return a basic fallback module for compilation
	pythonModule := &runtimePythonFallback{ModuleName: moduleName}
	return pythonModule, pythonInput
}
