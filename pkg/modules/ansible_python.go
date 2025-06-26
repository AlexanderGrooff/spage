package modules

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

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
	defer os.RemoveAll(tempDir)

	// Template the arguments with current facts
	templatedArgs := make(map[string]interface{})
	for k, v := range params.Args {
		if strVal, ok := v.(string); ok {
			templated, err := pkg.TemplateString(strVal, closure)
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

	// Ensure ANSIBLE_COLLECTIONS_PATH is passed through, checking for both new and old names.
	if path, ok := os.LookupEnv("ANSIBLE_COLLECTIONS_PATH"); ok {
		cmd.Env = append(cmd.Env, fmt.Sprintf("ANSIBLE_COLLECTIONS_PATH=%s", path))
	} else if path, ok := os.LookupEnv("ANSIBLE_COLLECTIONS_PATHS"); ok {
		cmd.Env = append(cmd.Env, fmt.Sprintf("ANSIBLE_COLLECTIONS_PATHS=%s", path))
	}

	// If we're executing on a remote host, we need to handle that differently
	if !closure.HostContext.Host.IsLocal {
		return m.executeRemotePythonModule(params, templatedArgs, closure, runAs)
	}

	output, err := cmd.CombinedOutput()

	common.DebugOutput("Ansible command output on host %s: %s", closure.HostContext.Host.Name, string(output))

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

func (m AnsiblePythonModule) executeRemotePythonModule(params AnsiblePythonInput, templatedArgs map[string]interface{}, closure *pkg.Closure, runAs string) (_ pkg.ModuleOutput, err error) {
	// --- BUNDLING LOGIC ---
	// Find local paths for ansible module_utils and collections to create a self-contained bundle
	cmd := exec.Command("python3", "-c", `import ansible; import os; print(os.path.dirname(ansible.__file__))`)
	ansiblePathBytes, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("could not find local ansible installation to bundle: %w: %s", err, string(ansiblePathBytes))
	}
	ansibleDir := strings.TrimSpace(string(ansiblePathBytes))
	ansibleBaseDir := filepath.Dir(ansibleDir)

	collectionsPath := os.Getenv("ANSIBLE_COLLECTIONS_PATH")
	if collectionsPath == "" {
		collectionsPath = os.Getenv("ANSIBLE_COLLECTIONS_PATHS") // fallback to old name
	} else {
		collectionsPath, err = filepath.Abs(collectionsPath)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for collections path: %w", err)
		}
	}

	// Create a temporary tarball locally
	localTarFile, err := os.CreateTemp("", "spage-ansible-bundle-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("failed to create local temp file for tarball: %w", err)
	}
	localTarPath := localTarFile.Name()
	defer os.Remove(localTarPath)
	localTarFile.Close()

	var tarArgs []string
	tarArgs = append(tarArgs, "-czf", localTarPath)
	tarArgs = append(tarArgs, "-C", ansibleBaseDir, "ansible/module_utils", "ansible/modules")
	if collectionsPath != "" {
		if _, err := os.Stat(filepath.Join(collectionsPath, "ansible_collections")); err == nil {
			tarArgs = append(tarArgs, "-C", collectionsPath, "ansible_collections")
		}
	}

	tarCmd := exec.Command("tar", tarArgs...)
	if output, err := tarCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to create ansible bundle tarball: %w: %s", err, string(output))
	}

	// Read tarball and base64 encode it for safe transport
	tarballBytes, err := os.ReadFile(localTarPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read local tarball: %w", err)
	}
	encodedTarball := base64.StdEncoding.EncodeToString(tarballBytes)

	// Create a unique temporary directory on the remote host
	remoteTempDir := fmt.Sprintf("/tmp/spage-exec-%d", time.Now().UnixNano())
	defer closure.HostContext.RunCommand(fmt.Sprintf("rm -rf %s", remoteTempDir), runAs) // Defer cleanup of the whole directory

	// Create the remote directory
	if rc, _, stderr, err := closure.HostContext.RunCommand(fmt.Sprintf("mkdir -p %s", remoteTempDir), runAs); err != nil || rc != 0 {
		return nil, fmt.Errorf("failed to create remote temp dir: rc=%d, err=%w, stderr=%s", rc, err, stderr)
	}

	// Write the base64 encoded tarball to a file on the remote host
	remoteB64Path := filepath.Join(remoteTempDir, "bundle.b64")
	if err := closure.HostContext.WriteFile(remoteB64Path, encodedTarball, runAs); err != nil {
		return nil, fmt.Errorf("failed to write ansible bundle to remote host: %w", err)
	}

	// Decode the tarball and unpack it
	remoteTarPath := filepath.Join(remoteTempDir, "bundle.tar.gz")
	unpackCmd := fmt.Sprintf("base64 -d %s > %s && tar -xzf %s -C %s", remoteB64Path, remoteTarPath, remoteTarPath, remoteTempDir)
	if rc, _, stderr, err := closure.HostContext.RunCommand(unpackCmd, runAs); err != nil || rc != 0 {
		return nil, fmt.Errorf("failed to unpack ansible bundle on remote host: rc=%d, err=%w, stderr=%s", rc, err, stderr)
	}
	// --- END BUNDLING LOGIC ---

	argsJSON, err := json.Marshal(templatedArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal args for remote execution: %w", err)
	}

	var pythonModulePath string
	moduleName := params.ModuleName
	if strings.Contains(moduleName, ".") {
		parts := strings.Split(moduleName, ".")
		if len(parts) < 3 {
			return nil, fmt.Errorf("invalid ansible FQCN '%s', expected format 'namespace.collection.module'", moduleName)
		}
		namespace := parts[0]
		collection := parts[1]
		moduleParts := parts[2:]
		pythonModulePath = fmt.Sprintf("ansible_collections.%s.%s.plugins.modules.%s", namespace, collection, strings.Join(moduleParts, "."))
	} else {
		pythonModulePath = "ansible.modules." + moduleName
	}

	pythonScript := fmt.Sprintf(`#!/usr/bin/env python3
import json
import sys
import importlib
import ansible.module_utils.basic

# Mock the AnsibleModule to capture the result
class MockAnsibleModule:
    def __init__(self, argument_spec, **kwargs):
        # We don't need to read from stdin because the params are injected directly from Spage.
        self.params = %s
        
    def exit_json(self, **kwargs):
        print(json.dumps(kwargs))
        sys.stdout.flush()
        sys.exit(0)
        
    def fail_json(self, **kwargs):
        kwargs['failed'] = True
        print(json.dumps(kwargs))
        sys.stdout.flush()
        sys.exit(1)

# Replace AnsibleModule with our mock class BEFORE importing the target module.
# This ensures that when the module does 'from ansible.module_utils.basic import AnsibleModule',
# it gets our mocked version.
ansible.module_utils.basic.AnsibleModule = MockAnsibleModule

try:
    # Now, import the module.
    ansible_module = importlib.import_module("%s")
    
    # Run the module's main() function.
    ansible_module.main()
    
except ImportError as e:
    print(json.dumps({"failed": True, "msg": "Module '%s' not found: " + str(e)}))
    sys.stdout.flush()
    sys.exit(1)
except Exception as e:
    print(json.dumps({"failed": True, "msg": f"Execution error in module '%s': {type(e).__name__}: {e}"}))
    sys.stdout.flush()
    sys.exit(1)
`, string(argsJSON), pythonModulePath, moduleName, moduleName)

	// Ensure the remote script is cleaned up regardless of what happens next.
	remotePath := "/tmp/spage-python-fallback.py"
	if err := closure.HostContext.WriteFile(remotePath, pythonScript, runAs); err != nil {
		return AnsiblePythonOutput{
			Failed: true,
			Msg:    fmt.Sprintf("Failed to write remote script: %v", err),
		}, nil
	}
	defer closure.HostContext.RunCommand(fmt.Sprintf("rm -f %s", remotePath), runAs)

	// Execute the script directly with the python3 interpreter.
	// This is more robust than making the script executable and relying on a shebang,
	// which seems to be failing in the remote environment.
	command := fmt.Sprintf("PYTHONPATH=%s /usr/bin/python3 %s", remoteTempDir, remotePath)
	rc, stdout, stderr, err := closure.HostContext.RunCommand(command, runAs)

	if err != nil {
		return AnsiblePythonOutput{
			Failed: true,
			Msg:    fmt.Sprintf("Remote execution failed: %v", err),
		}, fmt.Errorf("failed to execute module '%s' via Python fallback: %s. Stdout: %s, Stderr: %s", params.ModuleName, err, stdout, stderr)
	}

	// Try to parse JSON output even if the script failed. If parsing fails,
	// create our own JSON error to be parsed by the same logic below.
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		errorMsg := fmt.Sprintf("Remote script exited with code %d and non-JSON output. Stderr: %s. Stdout: %s", rc, stderr, stdout)
		errorJSON := fmt.Sprintf(`{"failed": true, "msg": %s}`, strconv.Quote(errorMsg))
		if err := json.Unmarshal([]byte(errorJSON), &result); err != nil {
			// This should never happen, but as a fallback, return a hardcoded error.
			return AnsiblePythonOutput{
				Failed: true,
				Msg:    fmt.Sprintf("Internal error: Could not parse fallback error JSON: %v", err),
			}, fmt.Errorf("failed to execute module '%s' and could not parse output: %s", params.ModuleName, err)
		}
	}

	// We have valid JSON, construct the output from it.
	output := AnsiblePythonOutput{
		Results: result,
	}
	if changed, ok := result["changed"].(bool); ok {
		output.WasChanged = changed
	}
	if failed, ok := result["failed"].(bool); ok {
		output.Failed = failed
	}
	// If the script exited non-zero but didn't set "failed: true" in the JSON, force it.
	if rc != 0 && !output.Failed {
		output.Failed = true
	}
	if msg, ok := result["msg"].(string); ok {
		output.Msg = msg
	}
	// If there's no message but we have stderr, use that.
	if output.Msg == "" && stderr != "" {
		output.Msg = stderr
	}

	return output, nil
}

func (m AnsiblePythonModule) parseAnsibleOutput(output, moduleName string) AnsiblePythonOutput {
	result := AnsiblePythonOutput{
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
