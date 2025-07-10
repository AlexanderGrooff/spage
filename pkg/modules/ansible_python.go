package modules

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"gopkg.in/yaml.v3"
)

// Cache for tracking what has been installed on each host
var (
	hostCollectionsCache = make(map[string]map[string]bool) // host -> collection -> installed
	hostBundleCache      = make(map[string]bool)            // host -> bundle_installed
	cacheMutex           sync.RWMutex
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

// Helper functions for cache management
func isCollectionCached(hostKey, collection string) bool {
	cacheMutex.RLock()
	defer cacheMutex.RUnlock()
	if hostColls, exists := hostCollectionsCache[hostKey]; exists {
		return hostColls[collection]
	}
	return false
}

func markCollectionCached(hostKey, collection string) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	if hostCollectionsCache[hostKey] == nil {
		hostCollectionsCache[hostKey] = make(map[string]bool)
	}
	hostCollectionsCache[hostKey][collection] = true
}

func isBundleCached(hostKey string) bool {
	cacheMutex.RLock()
	defer cacheMutex.RUnlock()
	return hostBundleCache[hostKey]
}

func markBundleCached(hostKey string) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	hostBundleCache[hostKey] = true
}

func getHostKey(host *pkg.Host) string {
	return host.Host
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
	hostKey := getHostKey(closure.HostContext.Host)

	common.LogDebug("Starting remote Python module execution", map[string]interface{}{
		"module": params.ModuleName,
		"host":   closure.HostContext.Host.Name,
	})

	// Ensure we have the required collections on the remote host (with caching)
	if err := m.ensureCollectionsOnRemote(params.ModuleName, closure, runAs, hostKey); err != nil {
		return nil, fmt.Errorf("failed to ensure collections on remote host: %w", err)
	}

	// Determine working directory with multi-level caching strategy
	var workingDir string
	var shouldCleanup bool

	// Level 1: Check for permanent cache
	if isBundleCached(hostKey) {
		workingDir = "/tmp/spage-ansible-cached"
		common.LogDebug("Using permanent cached ansible bundle", map[string]interface{}{
			"host": closure.HostContext.Host.Name,
			"path": workingDir,
		})
	} else {
		// Level 2: Check for session cache (predictable path)
		sessionCachePath := "/tmp/spage-ansible-session"
		checkSessionCmd := fmt.Sprintf("test -d %s/ansible && echo 'session_exists'", sessionCachePath)
		if rc, stdout, _, err := closure.HostContext.RunCommand(checkSessionCmd, runAs); err == nil && rc == 0 && strings.Contains(stdout, "session_exists") {
			workingDir = sessionCachePath
			common.LogDebug("Using session cached ansible bundle", map[string]interface{}{
				"host": closure.HostContext.Host.Name,
				"path": workingDir,
			})
		} else {
			// Level 3: Create new bundle (first time or session cache invalid)
			workingDir = sessionCachePath
			shouldCleanup = false // Keep session cache for subsequent calls

			// Create the session directory
			if rc, _, stderr, err := closure.HostContext.RunCommand(fmt.Sprintf("mkdir -p %s", workingDir), runAs); err != nil || rc != 0 {
				return nil, fmt.Errorf("failed to create session cache dir: rc=%d, err=%w, stderr=%s", rc, err, stderr)
			}

			// Transfer ansible bundle to session cache
			if err := m.transferMinimalAnsibleBundle(workingDir, closure, runAs, hostKey); err != nil {
				return nil, fmt.Errorf("failed to transfer ansible bundle: %w", err)
			}
		}
	}

	// Cleanup session cache at the end if needed
	if shouldCleanup {
		defer func() {
			if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("rm -rf %s", workingDir), runAs); err != nil {
				common.LogWarn("Failed to cleanup working directory", map[string]interface{}{
					"workingDir": workingDir,
					"error":      err,
				})
			}
		}()
	}

	// Generate the Python execution script
	pythonScript, err := m.generatePythonScript(params, templatedArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Python script: %w", err)
	}

	// Execute the module
	return m.executeRemoteScript(pythonScript, workingDir, closure, runAs, params.ModuleName)
}

// ensureCollectionsOnRemote checks if required collections exist and installs them if missing (with caching)
func (m AnsiblePythonModule) ensureCollectionsOnRemote(moduleName string, closure *pkg.Closure, runAs, hostKey string) error {
	// For core modules (no dots), no collections needed
	if !strings.Contains(moduleName, ".") {
		common.LogDebug("Core module, no collections required", map[string]interface{}{
			"module": moduleName,
			"host":   closure.HostContext.Host.Name,
		})
		return nil
	}

	// Parse collection from FQCN (e.g., "community.general.python_requirements_info" -> "community.general")
	parts := strings.Split(moduleName, ".")
	if len(parts) < 3 {
		return fmt.Errorf("invalid ansible FQCN '%s', expected format 'namespace.collection.module'", moduleName)
	}
	collectionName := strings.Join(parts[:2], ".")

	// Check cache first
	if isCollectionCached(hostKey, collectionName) {
		common.LogDebug("Collection already cached for host", map[string]interface{}{
			"collection": collectionName,
			"host":       closure.HostContext.Host.Name,
		})
		return nil
	}

	common.LogDebug("Checking for collection on remote host", map[string]interface{}{
		"collection": collectionName,
		"module":     moduleName,
		"host":       closure.HostContext.Host.Name,
	})

	// Check if collection already exists
	checkCmd := fmt.Sprintf("python3 -c \"import ansible_collections.%s.%s.plugins.modules.%s; print('exists')\" 2>/dev/null",
		parts[0], parts[1], parts[2])

	if rc, stdout, _, err := closure.HostContext.RunCommand(checkCmd, runAs); err == nil && rc == 0 && strings.Contains(stdout, "exists") {
		common.LogDebug("Collection already exists on remote host", map[string]interface{}{
			"collection": collectionName,
			"host":       closure.HostContext.Host.Name,
		})
		markCollectionCached(hostKey, collectionName)
		return nil
	}

	// Collection doesn't exist, try to install it
	common.LogInfo("Installing collection on remote host", map[string]interface{}{
		"collection": collectionName,
		"host":       closure.HostContext.Host.Name,
	})

	// Install collection with simplified command
	installCmd := fmt.Sprintf(`
		export PATH="$HOME/.local/bin:$PATH"
		if ! command -v ansible-galaxy >/dev/null 2>&1; then
			python3 -m pip install --user ansible-core >/dev/null 2>&1 || exit 1
			export PATH="$HOME/.local/bin:$PATH"
		fi
		ansible-galaxy collection install %s --force >/dev/null 2>&1
	`, collectionName)

	if rc, stdout, stderr, err := closure.HostContext.RunCommand(installCmd, runAs); err != nil || rc != 0 {
		common.LogWarn("Failed to install collection", map[string]interface{}{
			"collection": collectionName,
			"rc":         rc,
			"error":      fmt.Sprintf("stderr: %s, stdout: %s", stderr, stdout),
			"host":       closure.HostContext.Host.Name,
		})
		// Don't fail here - mark as attempted and let the module execution try
	} else {
		common.LogInfo("Successfully installed collection", map[string]interface{}{
			"collection": collectionName,
			"host":       closure.HostContext.Host.Name,
		})
	}

	// Mark as cached regardless of success to avoid repeated attempts
	markCollectionCached(hostKey, collectionName)
	return nil
}

// transferMinimalAnsibleBundle creates and transfers only the essential ansible files
func (m AnsiblePythonModule) transferMinimalAnsibleBundle(remoteTempDir string, closure *pkg.Closure, runAs, hostKey string) error {
	// Find local ansible installation
	cmd := exec.Command("python3", "-c", `import ansible; import os; print(os.path.dirname(ansible.__file__))`)
	ansiblePathBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not find local ansible installation: %w: %s", err, string(ansiblePathBytes))
	}
	ansibleDir := strings.TrimSpace(string(ansiblePathBytes))
	ansibleBaseDir := filepath.Dir(ansibleDir)

	common.LogDebug("Creating minimal ansible bundle", map[string]interface{}{
		"host": closure.HostContext.Host.Name,
	})

	// Create a much smaller tarball with only essential files
	localTarFile, err := os.CreateTemp("", "spage-ansible-minimal-*.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create local temp file for tarball: %w", err)
	}
	localTarPath := localTarFile.Name()
	defer func() {
		if err := os.Remove(localTarPath); err != nil {
			common.LogWarn("Failed to remove local tar file", map[string]interface{}{
				"file":  localTarPath,
				"error": err.Error(),
			})
		}
	}()
	if err := localTarFile.Close(); err != nil {
		return fmt.Errorf("failed to close local tar file: %w", err)
	}

	// Only include essential ansible core files (not collections)
	tarArgs := []string{
		"-czf", localTarPath,
		"-C", ansibleBaseDir,
		"ansible/module_utils", // Essential utilities
		"ansible/modules",      // Core modules
	}

	tarCmd := exec.Command("tar", tarArgs...)
	if output, err := tarCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create minimal ansible bundle: %w: %s", err, string(output))
	}

	// Read and transfer the tarball
	tarballBytes, err := os.ReadFile(localTarPath)
	if err != nil {
		return fmt.Errorf("failed to read tarball: %w", err)
	}

	common.LogDebug("Transferring minimal bundle", map[string]interface{}{
		"size": len(tarballBytes),
		"host": closure.HostContext.Host.Name,
	})

	encodedTarball := base64.StdEncoding.EncodeToString(tarballBytes)
	remoteB64Path := filepath.Join(remoteTempDir, "ansible-minimal.b64")

	if err := closure.HostContext.WriteFile(remoteB64Path, encodedTarball, runAs); err != nil {
		return fmt.Errorf("failed to write ansible bundle to remote host: %w", err)
	}

	// Decode and unpack on remote
	remoteTarPath := filepath.Join(remoteTempDir, "ansible-minimal.tar.gz")
	unpackCmd := fmt.Sprintf("base64 -d %s > %s && tar -xzf %s -C %s", remoteB64Path, remoteTarPath, remoteTarPath, remoteTempDir)

	if rc, _, stderr, err := closure.HostContext.RunCommand(unpackCmd, runAs); err != nil || rc != 0 {
		return fmt.Errorf("failed to unpack ansible bundle on remote host: rc=%d, err=%w, stderr=%s", rc, err, stderr)
	}

	// Cache the bundle for future use with improved error handling
	cachedPath := "/tmp/spage-ansible-cached"

	// Step 1: Clean up any existing cache directory
	cleanCmd := fmt.Sprintf("rm -rf %s", cachedPath)
	if rc, stdout, stderr, err := closure.HostContext.RunCommand(cleanCmd, runAs); err != nil || rc != 0 {
		common.LogDebug("Failed to clean cache directory", map[string]interface{}{
			"host":   closure.HostContext.Host.Name,
			"rc":     rc,
			"error":  err,
			"stderr": stderr,
			"stdout": stdout,
		})
		return nil // Don't fail the whole operation just because caching failed
	}

	// Step 2: Create the cache directory
	mkdirCmd := fmt.Sprintf("mkdir -p %s", cachedPath)
	if rc, stdout, stderr, err := closure.HostContext.RunCommand(mkdirCmd, runAs); err != nil || rc != 0 {
		common.LogDebug("Failed to create cache directory", map[string]interface{}{
			"host":   closure.HostContext.Host.Name,
			"rc":     rc,
			"error":  err,
			"stderr": stderr,
			"stdout": stdout,
		})
		return nil // Don't fail the whole operation just because caching failed
	}

	// Step 3: Copy files using a more reliable method (use . instead of /*)
	copyCmd := fmt.Sprintf("cp -r %s/. %s/", remoteTempDir, cachedPath)
	if rc, stdout, stderr, err := closure.HostContext.RunCommand(copyCmd, runAs); err != nil || rc != 0 {
		common.LogDebug("Failed to copy to cache directory", map[string]interface{}{
			"host":    closure.HostContext.Host.Name,
			"rc":      rc,
			"error":   err,
			"stderr":  stderr,
			"stdout":  stdout,
			"command": copyCmd,
		})
		return nil // Don't fail the whole operation just because caching failed
	}

	// Step 4: Verify the cache was created successfully
	verifyCmd := fmt.Sprintf("test -d %s/ansible && echo 'cache_ok'", cachedPath)
	if rc, stdout, stderr, err := closure.HostContext.RunCommand(verifyCmd, runAs); err == nil && rc == 0 && strings.Contains(stdout, "cache_ok") {
		markBundleCached(hostKey)
		common.LogDebug("Successfully cached ansible bundle for future use", map[string]interface{}{
			"host": closure.HostContext.Host.Name,
		})
	} else {
		common.LogDebug("Cache verification failed", map[string]interface{}{
			"host":   closure.HostContext.Host.Name,
			"rc":     rc,
			"error":  err,
			"stderr": stderr,
			"stdout": stdout,
		})
	}

	return nil
}

// generatePythonScript creates the Python execution script
func (m AnsiblePythonModule) generatePythonScript(params AnsiblePythonInput, templatedArgs map[string]interface{}) (string, error) {
	argsJSON, err := json.Marshal(templatedArgs)
	if err != nil {
		return "", fmt.Errorf("failed to marshal args: %w", err)
	}

	var pythonModulePath string
	moduleName := params.ModuleName
	if strings.Contains(moduleName, ".") {
		parts := strings.Split(moduleName, ".")
		if len(parts) < 3 {
			return "", fmt.Errorf("invalid ansible FQCN '%s', expected format 'namespace.collection.module'", moduleName)
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

# Replace AnsibleModule with our mock
ansible.module_utils.basic.AnsibleModule = MockAnsibleModule

try:
    ansible_module = importlib.import_module("%s")
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

	return pythonScript, nil
}

// executeRemoteScript executes the Python script on the remote host
func (m AnsiblePythonModule) executeRemoteScript(pythonScript, workingDir string, closure *pkg.Closure, runAs, moduleName string) (pkg.ModuleOutput, error) {
	// Write the script to remote
	remotePath := "/tmp/spage-python-fallback.py"
	if err := closure.HostContext.WriteFile(remotePath, pythonScript, runAs); err != nil {
		return AnsiblePythonOutput{Failed: true, Msg: fmt.Sprintf("Failed to write remote script: %v", err)}, nil
	}
	defer func() {
		if _, _, _, err := closure.HostContext.RunCommand(fmt.Sprintf("rm -f %s", remotePath), runAs); err != nil {
			common.LogWarn("Failed to cleanup remote script", map[string]interface{}{
				"remotePath": remotePath,
				"error":      err,
			})
		}
	}()

	// Build simplified PYTHONPATH command using the working directory
	pythonPathCmd := fmt.Sprintf(`#!/bin/sh
		PYTHONPATH="%s:$HOME/.local/lib/python3.*/site-packages:$HOME/.ansible/collections:/usr/local/lib/python3.*/dist-packages:/usr/lib/python3/dist-packages"
		export PYTHONPATH
		exec python3 %s
	`, workingDir, remotePath)

	rc, stdout, stderr, err := closure.HostContext.RunCommand(pythonPathCmd, runAs)

	if err != nil {
		return AnsiblePythonOutput{
			Failed: true,
			Msg:    fmt.Sprintf("Remote execution failed: %v", err),
		}, fmt.Errorf("failed to execute module '%s' via Python fallback: %s", moduleName, err)
	}

	// Parse JSON output
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		// Create simplified error message
		errorMsg := fmt.Sprintf("Module execution failed (exit code %d)", rc)
		if stderr != "" && len(stderr) < 100 {
			errorMsg += fmt.Sprintf(": %s", stderr)
		}
		if stdout != "" && len(stdout) < 100 {
			errorMsg += fmt.Sprintf(" [stdout: %s]", stdout)
		}

		return AnsiblePythonOutput{
			Failed: true,
			Msg:    errorMsg,
		}, fmt.Errorf("failed to execute module '%s': %s", moduleName, errorMsg)
	}

	// Construct output
	output := AnsiblePythonOutput{Results: result}
	if changed, ok := result["changed"].(bool); ok {
		output.WasChanged = changed
	}
	if failed, ok := result["failed"].(bool); ok {
		output.Failed = failed
	}
	if rc != 0 && !output.Failed {
		output.Failed = true
	}
	if msg, ok := result["msg"].(string); ok {
		output.Msg = msg
	}
	if output.Msg == "" && stderr != "" && len(stderr) < 200 {
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
