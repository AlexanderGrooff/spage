package compile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// fileSystem abstracts file system operations to support both disk and fs.FS
type fileSystem interface {
	ReadFile(path string) ([]byte, error)
	Stat(path string) (fs.FileInfo, error)
	IsLocal() bool
}

// diskFS implements fileSystem for local disk operations
type diskFS struct{}

func (d diskFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (d diskFS) Stat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

func (d diskFS) IsLocal() bool {
	return true
}

// fsWrapper implements fileSystem for fs.FS operations
type fsWrapper struct {
	fsys fs.FS
}

func (f fsWrapper) ReadFile(path string) ([]byte, error) {
	return fs.ReadFile(f.fsys, path)
}

func (f fsWrapper) Stat(path string) (fs.FileInfo, error) {
	return fs.Stat(f.fsys, path)
}

func (f fsWrapper) IsLocal() bool {
	return false
}

// splitRolesPaths splits a colon-delimited roles_path string into individual paths
func splitRolesPaths(rolesPaths string) []string {
	if rolesPaths == "" {
		return []string{"roles"} // Default to "roles" directory
	}
	paths := strings.Split(rolesPaths, ":")
	// Filter out empty paths
	var result []string
	for _, path := range paths {
		if strings.TrimSpace(path) != "" {
			result = append(result, strings.TrimSpace(path))
		}
	}
	if len(result) == 0 {
		return []string{"roles"} // Fallback to default if all paths are empty
	}
	return result
}

// findRoleInPaths searches for a role in the provided roles paths
func findRoleInPaths(fsys fileSystem, roleName string, rolesPaths []string, currentBasePath string) (string, error) {
	for _, rolesPath := range rolesPaths {
		var rolePath string
		if filepath.IsAbs(rolesPath) {
			rolePath = filepath.Join(rolesPath, roleName, "tasks")
		} else {
			rolePath = filepath.Join(currentBasePath, rolesPath, roleName, "tasks")
		}

		// Convert to forward slashes for fs.FS compatibility
		rolePath = filepath.ToSlash(rolePath)

		// Check if main.yaml or main.yml exists
		if _, err := fsys.Stat(filepath.Join(rolePath, "main.yaml")); err == nil {
			return rolePath, nil
		}
		if _, err := fsys.Stat(filepath.Join(rolePath, "main.yml")); err == nil {
			return rolePath, nil
		}
	}
	return "", fmt.Errorf("role '%s' not found in any of the roles paths: %v", roleName, rolesPaths)
}

// readRoleData reads role task data from the given role directory
func readRoleData(fsys fileSystem, roleDir string) ([]byte, error) {
	// Try yaml then yml
	if b, err := fsys.ReadFile(filepath.Join(roleDir, "main.yaml")); err == nil {
		return b, nil
	}
	if b, err := fsys.ReadFile(filepath.Join(roleDir, "main.yml")); err == nil {
		return b, nil
	}
	return nil, fmt.Errorf("role tasks file not found in %s", roleDir)
}

// readRoleResourceData reads data from a specific role resource directory (defaults, vars, handlers, etc.)
func readRoleResourceData(fsys fileSystem, roleBaseDir, resourceType string) ([]byte, error) {
	resourceDir := filepath.Join(roleBaseDir, resourceType)

	// Try yaml then yml
	if b, err := fsys.ReadFile(filepath.Join(resourceDir, "main.yaml")); err == nil {
		return b, nil
	}
	if b, err := fsys.ReadFile(filepath.Join(resourceDir, "main.yml")); err == nil {
		return b, nil
	}
	// Return nil if resource file doesn't exist (it's optional)
	return nil, nil
}

// loadRoleDefaults loads role defaults and returns them as a vars block
func loadRoleDefaults(fsys fileSystem, roleBaseDir string) (map[string]interface{}, error) {
	defaultsData, err := readRoleResourceData(fsys, roleBaseDir, "defaults")
	if err != nil {
		return nil, fmt.Errorf("failed to read role defaults: %w", err)
	}
	if defaultsData == nil {
		return nil, nil // No defaults file
	}

	var defaults map[string]interface{}
	if err := yaml.Unmarshal(defaultsData, &defaults); err != nil {
		return nil, fmt.Errorf("failed to parse role defaults YAML: %w", err)
	}
	return defaults, nil
}

// loadRoleVars loads role vars and returns them as a vars block
func loadRoleVars(fsys fileSystem, roleBaseDir string) (map[string]interface{}, error) {
	varsData, err := readRoleResourceData(fsys, roleBaseDir, "vars")
	if err != nil {
		return nil, fmt.Errorf("failed to read role vars: %w", err)
	}
	if varsData == nil {
		return nil, nil // No vars file
	}

	var vars map[string]interface{}
	if err := yaml.Unmarshal(varsData, &vars); err != nil {
		return nil, fmt.Errorf("failed to parse role vars YAML: %w", err)
	}
	return vars, nil
}

// loadRoleHandlers loads role handlers and returns them as handler blocks
func loadRoleHandlers(fsys fileSystem, roleBaseDir string, rolesPaths []string) ([]map[string]interface{}, error) {
	handlersData, err := readRoleResourceData(fsys, roleBaseDir, "handlers")
	if err != nil {
		return nil, fmt.Errorf("failed to read role handlers: %w", err)
	}
	if handlersData == nil {
		return nil, nil // No handlers file
	}

	// Parse and preprocess the handlers
	handlerBlocks, err := preprocessPlaybook(fsys, handlersData, filepath.Join(roleBaseDir, "handlers"), rolesPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to preprocess role handlers: %w", err)
	}

	// Mark all blocks as handlers
	for i := range handlerBlocks {
		handlerBlocks[i]["is_handler"] = true
	}

	return handlerBlocks, nil
}

// processIncludeDirective handles the 'include' directive during preprocessing.
func processIncludeDirective(fsys fileSystem, includeValue interface{}, currentBasePath string, rolesPaths []string) ([]map[string]interface{}, error) {
	if pathStr, ok := includeValue.(string); ok {
		absPath := pathStr
		if !filepath.IsAbs(pathStr) {
			absPath = filepath.ToSlash(filepath.Join(currentBasePath, pathStr))
		}
		includedData, err := fsys.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read included file %s: %w", absPath, err)
		}
		// Recursively preprocess the included content, using the included file's directory as the new base path
		nestedBasePath := filepath.ToSlash(filepath.Dir(absPath))
		nestedBlocks, err := preprocessPlaybook(fsys, includedData, nestedBasePath, rolesPaths)
		if err != nil {
			return nil, fmt.Errorf("failed to preprocess included file %s: %w", absPath, err)
		}
		return nestedBlocks, nil
	}
	return nil, fmt.Errorf("invalid 'include' value: expected string, got %T", includeValue)
}

// processIncludeRoleDirective handles the 'include_role' directive during preprocessing.
func processIncludeRoleDirective(fsys fileSystem, roleParams interface{}, currentBasePath string, rolesPaths []string) ([]map[string]interface{}, error) {
	paramsMap, ok := roleParams.(map[string]interface{})
	if !ok {
		// Handle simple string form: include_role: my_role_name
		if roleNameStr, okStr := roleParams.(string); okStr {
			paramsMap = map[string]interface{}{"name": roleNameStr}
		} else {
			return nil, fmt.Errorf("invalid 'include_role' value: expected map or string, got %T", roleParams)
		}
	}

	roleName, nameOk := paramsMap["name"].(string)
	if !nameOk || roleName == "" {
		return nil, fmt.Errorf("missing or invalid 'name' in include_role directive")
	}

	roleTasksBasePath, err := findRoleInPaths(fsys, roleName, rolesPaths, currentBasePath)
	if err != nil {
		return nil, err
	}

	// Get the role base directory (parent of tasks directory)
	roleBaseDir := filepath.Dir(roleTasksBasePath)

	var result []map[string]interface{}

	// 1. Load role defaults (lowest precedence)
	roleDefaults, err := loadRoleDefaults(fsys, roleBaseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load role '%s' defaults: %w", roleName, err)
	}
	if roleDefaults != nil {
		defaultsBlock := map[string]interface{}{
			"is_role_defaults": true,
			"role_name":        roleName,
			"vars":             roleDefaults,
		}
		result = append(result, defaultsBlock)
	}

	// 2. Load role vars (higher precedence than defaults)
	roleVars, err := loadRoleVars(fsys, roleBaseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load role '%s' vars: %w", roleName, err)
	}
	if roleVars != nil {
		varsBlock := map[string]interface{}{
			"is_role_vars": true,
			"role_name":    roleName,
			"vars":         roleVars,
		}
		result = append(result, varsBlock)
	}

	// 3. Load role tasks
	roleData, err := readRoleData(fsys, roleTasksBasePath)
	if err != nil {
		return nil, err
	}

	// Recursively preprocess the role's tasks, using the role's tasks directory as the base path
	roleBlocks, err := preprocessPlaybook(fsys, roleData, roleTasksBasePath, rolesPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to preprocess role '%s' tasks from %s: %w", roleName, roleTasksBasePath, err)
	}

	// Add role context information to each task block
	for i := range roleBlocks {
		if !isRoleDefaultsBlock(roleBlocks[i]) && !isRoleVarsBlock(roleBlocks[i]) && !isRootBlock(roleBlocks[i]) {
			// This is a task block - add role context
			roleBlocks[i]["_role_name"] = roleName
			roleBlocks[i]["_role_path"] = roleBaseDir
		}
	}
	result = append(result, roleBlocks...)

	// 4. Load role handlers (executed at the end)
	roleHandlers, err := loadRoleHandlers(fsys, roleBaseDir, rolesPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to load role '%s' handlers: %w", roleName, err)
	}
	if roleHandlers != nil {
		result = append(result, roleHandlers...)
	}

	return result, nil
}

// processPlaybookRoot handles the root level of an Ansible playbook with 'hosts' field
// and either 'roles' or 'tasks' sections.
func processPlaybookRoot(fsys fileSystem, playbookRoot map[string]interface{}, currentBasePath string, rolesPaths []string) ([]map[string]interface{}, error) {
	// Check if this is a playbook root entry (has 'hosts' field)
	if _, hasHosts := playbookRoot["hosts"]; !hasHosts {
		return nil, fmt.Errorf("not a playbook root entry: missing 'hosts' field")
	}

	var result []map[string]interface{}
	root_block := map[string]interface{}{
		"is_root": true,
	}

	// Add vars to the root block
	if vars, ok := playbookRoot["vars"]; ok {
		root_block["vars"] = vars
	}

	// Honor play-level connection: if present, inject into vars as ansible_connection
	if connRaw, hasConn := playbookRoot["connection"]; hasConn {
		if connStr, ok := connRaw.(string); ok && connStr != "" {
			// Ensure vars map exists
			if _, ok := root_block["vars"]; !ok {
				root_block["vars"] = map[string]interface{}{}
			}
			if varsMap, ok := root_block["vars"].(map[string]interface{}); ok {
				varsMap["ansible_connection"] = connStr
				// Also store as 'connection' var for compatibility
				varsMap["connection"] = connStr
				root_block["vars"] = varsMap
			}
		}
	}

	result = append(result, root_block)

    // Prepare registry once for processing meta directives inside task lists
    registry := getPreprocessorRegistry()

    // Process 'roles' section if it exists
    if roles, hasRoles := playbookRoot["roles"]; hasRoles {
        rolesList, ok := roles.([]interface{})
        if !ok {
            return nil, fmt.Errorf("invalid 'roles' section: expected list, got %T", roles)
        }

        for _, roleEntry := range rolesList {
            var roleName string

            // Handle both simple string role names and role entries with parameters
            switch role := roleEntry.(type) {
            case string:
                roleName = role
            case map[string]interface{}:
                if name, ok := role["role"].(string); ok {
                    roleName = name
                } else if name, ok := role["name"].(string); ok {
                    roleName = name
                } else {
                    return nil, fmt.Errorf("invalid role entry: missing 'role' or 'name' field")
                }
            default:
                return nil, fmt.Errorf("invalid role entry: expected string or map, got %T", roleEntry)
            }

            // Use the existing include_role processor to handle the role
            roleParams := map[string]interface{}{"name": roleName}
            roleBlocks, err := processIncludeRoleDirective(fsys, roleParams, currentBasePath, rolesPaths)
            if err != nil {
                return nil, fmt.Errorf("failed to process role '%s': %w", roleName, err)
            }
            result = append(result, roleBlocks...)
        }
    }

    // Process 'pre_tasks' section if it exists
    if preTasks, hasPreTasks := playbookRoot["pre_tasks"]; hasPreTasks {
        taskList, ok := preTasks.([]interface{})
        if !ok {
            return nil, fmt.Errorf("invalid 'pre_tasks' section: expected list, got %T", preTasks)
        }

        for _, taskEntry := range taskList {
            taskMap, ok := taskEntry.(map[string]interface{})
            if !ok {
                return nil, fmt.Errorf("invalid task entry: expected map, got %T", taskEntry)
            }

            // Try to process meta directives inside the task
            processed := false
            for key, value := range taskMap {
                if processor, ok := registry[key]; ok {
                    nestedBlocks, err := processor(fsys, value, currentBasePath, rolesPaths)
                    if err != nil {
                        return nil, fmt.Errorf("failed to process '%s' in pre_tasks: %w", key, err)
                    }
                    result = append(result, nestedBlocks...)
                    processed = true
                    break
                }
            }
            if !processed {
                // Each task is already a map, so just add it directly to the result
                result = append(result, taskMap)
            }
        }
    }

    // Process 'tasks' section if it exists
    if tasks, hasTasks := playbookRoot["tasks"]; hasTasks {
        tasksList, ok := tasks.([]interface{})
        if !ok {
            return nil, fmt.Errorf("invalid 'tasks' section: expected list, got %T", tasks)
        }

        for _, taskEntry := range tasksList {
            taskMap, ok := taskEntry.(map[string]interface{})
            if !ok {
                return nil, fmt.Errorf("invalid task entry: expected map, got %T", taskEntry)
            }

            // Try to process meta directives inside the task
            processed := false
            for key, value := range taskMap {
                if processor, ok := registry[key]; ok {
                    nestedBlocks, err := processor(fsys, value, currentBasePath, rolesPaths)
                    if err != nil {
                        return nil, fmt.Errorf("failed to process '%s' in tasks: %w", key, err)
                    }
                    result = append(result, nestedBlocks...)
                    processed = true
                    break
                }
            }
            if !processed {
                // Each task is already a map, so just add it directly to the result
                result = append(result, taskMap)
            }
        }
    }

    // Process 'post_tasks' section if it exists
    if postTasks, haspostTasks := playbookRoot["post_tasks"]; haspostTasks {
        taskList, ok := postTasks.([]interface{})
        if !ok {
            return nil, fmt.Errorf("invalid 'post_tasks' section: expected list, got %T", postTasks)
        }

        for _, taskEntry := range taskList {
            taskMap, ok := taskEntry.(map[string]interface{})
            if !ok {
                return nil, fmt.Errorf("invalid task entry: expected map, got %T", taskEntry)
            }

            // Try to process meta directives inside the task
            processed := false
            for key, value := range taskMap {
                if processor, ok := registry[key]; ok {
                    nestedBlocks, err := processor(fsys, value, currentBasePath, rolesPaths)
                    if err != nil {
                        return nil, fmt.Errorf("failed to process '%s' in post_tasks: %w", key, err)
                    }
                    result = append(result, nestedBlocks...)
                    processed = true
                    break
                }
            }
            if !processed {
                // Each task is already a map, so just add it directly to the result
                result = append(result, taskMap)
            }
        }
    }

    // Process 'handlers' section if it exists
    if handlers, hasHandlers := playbookRoot["handlers"]; hasHandlers {
        handlersList, ok := handlers.([]interface{})
        if !ok {
            return nil, fmt.Errorf("invalid 'handlers' section: expected list, got %T", handlers)
        }

        for _, handlerEntry := range handlersList {
            handlerMap, ok := handlerEntry.(map[string]interface{})
            if !ok {
                return nil, fmt.Errorf("invalid handler entry: expected map, got %T", handlerEntry)
            }

            // Try to process meta directives inside the handler
            processed := false
            for key, value := range handlerMap {
                if processor, ok := registry[key]; ok {
                    nestedBlocks, err := processor(fsys, value, currentBasePath, rolesPaths)
                    if err != nil {
                        return nil, fmt.Errorf("failed to process '%s' in handlers: %w", key, err)
                    }
                    // Mark all resulting blocks as handlers
                    for i := range nestedBlocks {
                        nestedBlocks[i]["is_handler"] = true
                    }
                    result = append(result, nestedBlocks...)
                    processed = true
                    break
                }
            }
            if !processed {
                // Mark this task as a handler
                handlerMap["is_handler"] = true
                result = append(result, handlerMap)
            }
        }
    }

    return result, nil
}

// Block type detection functions
func isRootBlock(block map[string]interface{}) bool {
	return block["is_root"] == true
}

func isRoleDefaultsBlock(block map[string]interface{}) bool {
	return block["is_role_defaults"] == true
}

func isRoleVarsBlock(block map[string]interface{}) bool {
    return block["is_role_vars"] == true
}

// preprocessorFunc defines the signature for functions that handle meta directives.
type preprocessorFunc func(fsys fileSystem, value interface{}, basePath string, rolesPaths []string) ([]map[string]interface{}, error)

// getPreprocessorRegistry returns the mapping of meta directive keywords to their processing functions.
func getPreprocessorRegistry() map[string]preprocessorFunc {
	return map[string]preprocessorFunc{
		"include":         processIncludeDirective,
		"import_tasks":    processIncludeDirective,
		"import_playbook": processIncludeDirective,
		"include_tasks":   processIncludeDirective,
		"include_role":    processIncludeRoleDirective,
		"import_role":     processIncludeRoleDirective,
	}
}

func getRegistryKeys(registry map[string]preprocessorFunc) []string {
	keys := make([]string, 0, len(registry))
	for k := range registry {
		keys = append(keys, k)
	}
	return keys
}

// preprocessPlaybook takes raw playbook YAML data and a base path,
// recursively processes registered meta directives (include, import_tasks, etc.),
// and returns a flattened list of raw task maps ready for parsing.
func preprocessPlaybook(fsys fileSystem, data []byte, basePath string, rolesPaths []string) ([]map[string]interface{}, error) {
	var initialBlocks []map[string]interface{}
	err := yaml.Unmarshal(data, &initialBlocks)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling YAML: %w", err)
	}

	var processedBlocks []map[string]interface{}
	var processErrors []error

	for _, block := range initialBlocks {
		// First check if this block is a playbook root entry (has 'hosts' field)
		if _, hasHosts := block["hosts"]; hasHosts {
			rootBlocks, err := processPlaybookRoot(fsys, block, basePath, rolesPaths)
			if err != nil {
				processErrors = append(processErrors, fmt.Errorf("error processing playbook root: %w", err))
			} else {
				processedBlocks = append(processedBlocks, rootBlocks...)
			}
			continue
		}

		// If not a playbook root, process as before
		processed := false
		registry := getPreprocessorRegistry()
		for key, value := range block {
			if processor, ok := registry[key]; ok {
				nestedBlocks, err := processor(fsys, value, basePath, rolesPaths)
				if err != nil {
					// Add context to the error, e.g., which directive failed
					processErrors = append(processErrors, fmt.Errorf("error processing '%s' directive: %w", key, err))
				} else {
					processedBlocks = append(processedBlocks, nestedBlocks...)
				}
				processed = true
				// Assume a block is either a meta directive OR a task, not both.
				// If a meta key is found, stop checking other keys in this block.
				break
			}
		}

		// If no registered meta directive key was found in the block, treat it as a standard task.
		if !processed {
			processedBlocks = append(processedBlocks, block)
		}
	}

	if len(processErrors) > 0 {
		errorMessages := make([]string, len(processErrors))
		for i, e := range processErrors {
			errorMessages[i] = e.Error()
		}
		return nil, fmt.Errorf("errors during preprocessing:\n%s", strings.Join(errorMessages, "\n"))
	}

	return processedBlocks, nil
}

// PreprocessPlaybook is the public interface for disk-based preprocessing
func PreprocessPlaybook(data []byte, basePath string, rolesPaths []string) ([]map[string]interface{}, error) {
	return preprocessPlaybook(diskFS{}, data, basePath, rolesPaths)
}

// PreprocessPlaybookFS is the public interface for fs.FS-based preprocessing
func PreprocessPlaybookFS(fsys fs.FS, data []byte, basePath string, rolesPaths []string) ([]map[string]interface{}, error) {
	return preprocessPlaybook(fsWrapper{fsys: fsys}, data, basePath, rolesPaths)
}
