package modules

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"gopkg.in/yaml.v3"
)

// AnsiblePythonInput defines parameters for executing Python Ansible modules
type AnsiblePythonInput struct {
	ModuleName string                 `yaml:"module_name" json:"module_name"`
	Args       map[string]interface{} `yaml:"args" json:"args"`
}

func (i AnsiblePythonInput) ToCode() string {
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

func (i AnsiblePythonInput) GetVariableUsage() []string {
	var variables []string
	// Extract variables from arguments recursively
	for _, v := range i.Args {
		if str, ok := v.(string); ok {
			variables = append(variables, pkg.GetVariableUsageFromTemplate(str)...)
		}
	}
	return variables
}

func (i AnsiblePythonInput) Validate() error {
	if i.ModuleName == "" {
		return fmt.Errorf("module_name is required for ansible_python module")
	}
	return nil
}

func (i AnsiblePythonInput) HasRevert() bool {
	return false // Python modules don't support revert
}

func (i AnsiblePythonInput) ProvidesVariables() []string {
	return nil // We don't know what variables a Python module might provide
}

// AnsiblePythonOutput defines the output from a Python Ansible module execution
type AnsiblePythonOutput struct {
	WasChanged bool                   `yaml:"changed" json:"changed"`
	Failed     bool                   `yaml:"failed" json:"failed"`
	Msg        string                 `yaml:"msg" json:"msg"`
	Results    map[string]interface{} `yaml:"results" json:"results"`
	pkg.ModuleOutput
}

func (o AnsiblePythonOutput) String() string {
	status := "ok"
	if o.Failed {
		status = "failed"
	} else if o.WasChanged {
		status = "changed"
	}
	return fmt.Sprintf("  status: %s\n  msg: %q\n  changed: %v\n", status, o.Msg, o.WasChanged)
}

func (o AnsiblePythonOutput) Changed() bool {
	return o.WasChanged
}

// AsFacts implements the pkg.FactProvider interface.
func (o AnsiblePythonOutput) AsFacts() map[string]interface{} {
	facts := make(map[string]interface{})
	facts["changed"] = o.Changed()
	facts["failed"] = o.Failed
	facts["msg"] = o.Msg

	// Flatten the results into the top level
	if o.Results != nil {
		common.LogDebug("Flattening Python module results", map[string]interface{}{
			"results": o.Results,
		})
		maps.Copy(facts, o.Results)
	}

	return facts
}

// AnsiblePythonModule implements a fallback module that executes Ansible modules via Python
type AnsiblePythonModule struct {
	ModuleName string
}

func (m AnsiblePythonModule) InputType() reflect.Type {
	return reflect.TypeOf(AnsiblePythonInput{})
}

func (m AnsiblePythonModule) OutputType() reflect.Type {
	return reflect.TypeOf(AnsiblePythonOutput{})
}

func (m AnsiblePythonModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	pythonParams, ok := params.(AnsiblePythonInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected AnsiblePythonInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected AnsiblePythonInput, got %T", params)
	}

	if err := pythonParams.Validate(); err != nil {
		return nil, err
	}

	common.LogInfo("Executing Ansible module via Python", map[string]interface{}{
		"module": pythonParams.ModuleName,
		"host":   closure.HostContext.Host.Name,
	})

	return m.executePythonModule(pythonParams, closure, runAs)
}

func (m AnsiblePythonModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	return nil, fmt.Errorf("revert is not supported for Python Ansible modules")
}

func (m AnsiblePythonModule) executePythonModule(params AnsiblePythonInput, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	// Create a temporary directory for the Ansible execution
	tempDir, err := os.MkdirTemp("", "spage-ansible-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			common.LogWarn("Failed to remove temp directory", map[string]interface{}{
				"tempDir": tempDir,
				"error":   err.Error(),
			})
		}
	}()

	templatedArgs := params.Args

	// Create the ansible-playbook YAML file
	hostname := closure.HostContext.Host.Host
	connection := "ssh"
	if hostname == "localhost" {
		connection = "local"
	} else if connVal, ok := closure.GetFact("ansible_connection"); ok {
		connection = connVal.(string)
	}
	playbook := map[string]interface{}{
		"hosts":        hostname,
		"gather_facts": false,
		"connection":   connection,
		"tasks": []map[string]interface{}{
			{
				"name":            fmt.Sprintf("Execute %s", params.ModuleName),
				params.ModuleName: templatedArgs,
				"become":          runAs != "",
				"become_user":     runAs,
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
	inventoryContent := map[string]interface{}{
		"all": map[string]interface{}{
			"hosts": map[string]interface{}{
				hostname: map[string]interface{}{
					"ansible_connection": connection,
				},
			},
		},
	}
	inventoryContentBytes, err := yaml.Marshal(inventoryContent)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal inventory: %w", err)
	}

	inventoryFile := filepath.Join(tempDir, "inventory")
	if err := os.WriteFile(inventoryFile, inventoryContentBytes, 0644); err != nil {
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

	// Ensure ANSIBLE_COLLECTIONS_PATH is passed through, checking for both new and old names.
	if path, ok := os.LookupEnv("ANSIBLE_COLLECTIONS_PATH"); ok {
		cmd.Env = append(cmd.Env, fmt.Sprintf("ANSIBLE_COLLECTIONS_PATH=%s", path))
	} else if path, ok := os.LookupEnv("ANSIBLE_COLLECTIONS_PATHS"); ok {
		cmd.Env = append(cmd.Env, fmt.Sprintf("ANSIBLE_COLLECTIONS_PATHS=%s", path))
	}

	output, err := cmd.CombinedOutput()

	common.DebugOutput("Ansible command output on host %s with connection %s: %s", closure.HostContext.Host.Name, connection, string(output))

	result := m.parseAnsibleOutput(string(output), params.ModuleName)

	if err != nil {
		// ansible-playbook failed with non-zero exit code
		if result.Msg == "" {
			result.Msg = fmt.Sprintf("ansible-playbook execution failed: %v", err)
		}
		result.Failed = true
		return result, fmt.Errorf("failed to execute module '%s' via Python fallback: %s", params.ModuleName, result.Msg)
	}

	// Check if the module itself reported a failure even with zero exit code
	if result.Failed {
		return result, fmt.Errorf("failed to execute module '%s' via Python fallback: %s", params.ModuleName, result.Msg)
	}

	return result, nil
}

func (m AnsiblePythonModule) parseAnsibleOutput(output, moduleName string) AnsiblePythonOutput {
	result := AnsiblePythonOutput{
		Results: make(map[string]interface{}),
	}

	lines := strings.Split(output, "\n")

	// Helper to count braces outside of string literals
	countBraces := func(s string) int {
		depth := 0
		inStr := false
		escaped := false
		for _, r := range s {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' && inStr {
				escaped = true
				continue
			}
			if r == '"' {
				inStr = !inStr
				continue
			}
			if !inStr {
				switch r {
				case '{':
					depth++
				case '}':
					depth--
				}
			}
		}
		return depth
	}

	var buf strings.Builder
	accumulating := false
	braceDepth := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Look for JSON output in ansible verbose mode (single- or multi-line)
		if !accumulating {
			if strings.Contains(line, "=>") && strings.Contains(line, "{") {
				jsonStart := strings.Index(line, "{")
				if jsonStart != -1 {
					fragment := line[jsonStart:]
					buf.WriteString(fragment)
					braceDepth += countBraces(fragment)
					accumulating = true

					// If JSON is complete on one line, parse immediately
					if braceDepth == 0 {
						jsonStr := buf.String()
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
						} else {
							// Reset and continue scanning other lines
							buf.Reset()
							accumulating = false
							braceDepth = 0
						}
					}
					// Continue to next line to accumulate multi-line JSON
					continue
				}
			}
		} else {
			// Accumulate subsequent lines until braces balance
			buf.WriteString("\n")
			buf.WriteString(line)
			braceDepth += countBraces(line)
			if braceDepth <= 0 {
				jsonStr := buf.String()
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
				// Parsing failed: reset accumulation and fall through to other checks
				buf.Reset()
				accumulating = false
				braceDepth = 0
			}
			// Move to next line while accumulating
			continue
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

func (m AnsiblePythonModule) ParameterAliases() map[string]string {
	return nil // No aliases for the Python module wrapper
}

// PythonFallbackModule implements Module interface for Python fallback handling
type PythonFallbackModule struct{}

func (m PythonFallbackModule) InputType() reflect.Type {
	return reflect.TypeOf(AnsiblePythonInput{})
}

func (m PythonFallbackModule) OutputType() reflect.Type {
	return reflect.TypeOf(AnsiblePythonOutput{})
}

func (m PythonFallbackModule) Execute(params pkg.ConcreteModuleInputProvider, c *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	pythonInput, ok := params.(*AnsiblePythonInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type for Python fallback module: %T", params)
	}
	// Use the existing AnsiblePythonModule's method
	ansibleModule := AnsiblePythonModule{}
	return ansibleModule.executePythonModule(*pythonInput, c, runAs)
}

func (m PythonFallbackModule) Revert(params pkg.ConcreteModuleInputProvider, c *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	// Python modules don't support revert
	return AnsiblePythonOutput{
		WasChanged: false,
		Failed:     false,
		Results:    make(map[string]interface{}),
	}, nil
}

func (m PythonFallbackModule) ParameterAliases() map[string]string {
	return nil
}

// GetPythonFallbackForCompilation creates a Python fallback module and params for compilation phase
func GetPythonFallbackForCompilation(moduleName string, rawParams interface{}) (pkg.Module, interface{}) {
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

	// Create the AnsiblePythonInput structure directly
	pythonInput := AnsiblePythonInput{
		ModuleName: moduleName,
		Args:       paramsMap,
	}

	return PythonFallbackModule{}, pythonInput
}

// GetPythonFallbackModule creates a Python fallback module for unknown modules
func GetPythonFallbackModule(moduleName string, originalParams pkg.ConcreteModuleInputProvider) (pkg.Module, pkg.ConcreteModuleInputProvider, error) {
	pythonModule := AnsiblePythonModule{ModuleName: moduleName}

	pythonInput := AnsiblePythonInput{
		ModuleName: moduleName,
		Args:       make(map[string]interface{}),
	}

	// Convert existing params to a map for the Python module
	if originalParams != nil {
		paramsMap, err := convertStructToMap(originalParams)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to convert module params for Python fallback: %w", err)
		}
		pythonInput.Args = paramsMap
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

func init() {
	pkg.RegisterModule("ansible_python", AnsiblePythonModule{})
}
