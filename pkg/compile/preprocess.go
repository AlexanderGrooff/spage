package compile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

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
func findRoleInPaths(roleName string, rolesPaths []string, currentBasePath string) (string, error) {
	for _, rolesPath := range rolesPaths {
		var rolePath string
		if filepath.IsAbs(rolesPath) {
			rolePath = filepath.Join(rolesPath, roleName, "tasks")
		} else {
			rolePath = filepath.Join(currentBasePath, rolesPath, roleName, "tasks")
		}

		// Check if main.yaml or main.yml exists
		if _, err := os.Stat(filepath.Join(rolePath, "main.yaml")); err == nil {
			return rolePath, nil
		}
		if _, err := os.Stat(filepath.Join(rolePath, "main.yml")); err == nil {
			return rolePath, nil
		}
	}
	return "", fmt.Errorf("role '%s' not found in any of the roles paths: %v", roleName, rolesPaths)
}

// processIncludeDirective handles the 'include' directive during preprocessing.
func processIncludeDirective(includeValue interface{}, currentBasePath string, rolesPaths []string) ([]map[string]interface{}, error) {
	if pathStr, ok := includeValue.(string); ok {
		absPath := pathStr
		if !filepath.IsAbs(pathStr) {
			absPath = filepath.Join(currentBasePath, pathStr)
		}
		includedData, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read included file %s: %w", absPath, err)
		}
		// Recursively preprocess the included content, using the included file's directory as the new base path
		nestedBasePath := filepath.Dir(absPath)
		nestedBlocks, err := PreprocessPlaybook(includedData, nestedBasePath, rolesPaths)
		if err != nil {
			return nil, fmt.Errorf("failed to preprocess included file %s: %w", absPath, err)
		}
		return nestedBlocks, nil
	} else {
		return nil, fmt.Errorf("invalid 'include' value: expected string, got %T", includeValue)
	}
}

// processImportTasksDirective handles the 'import_tasks' directive during preprocessing.
// In this static preprocessing model, it behaves identically to processIncludeDirective.
func processImportTasksDirective(importValue interface{}, currentBasePath string, rolesPaths []string) ([]map[string]interface{}, error) {
	if pathStr, ok := importValue.(string); ok {
		absPath := pathStr
		if !filepath.IsAbs(pathStr) {
			absPath = filepath.Join(currentBasePath, pathStr)
		}
		importedData, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read imported tasks file %s: %w", absPath, err)
		}
		// Recursively preprocess the imported content
		nestedBasePath := filepath.Dir(absPath)
		nestedBlocks, err := PreprocessPlaybook(importedData, nestedBasePath, rolesPaths)
		if err != nil {
			return nil, fmt.Errorf("failed to preprocess imported tasks file %s: %w", absPath, err)
		}
		return nestedBlocks, nil
	} else {
		return nil, fmt.Errorf("invalid 'import_tasks' value: expected string, got %T", importValue)
	}
}

func readRoleData(roleDir string) ([]byte, error) {
	// Assume roles are in a 'roles' directory relative to the current base path.
	// TODO: Make roles path configurable.

	// Read from yml or yaml
	roleTasksPath := filepath.Join(roleDir, "main.yaml")

	roleData, err := os.ReadFile(roleTasksPath)
	if err != nil {
		if os.IsNotExist(err) {
			// .yaml doesn't exist, let's try .yml
			roleTasksPath = filepath.Join(roleDir, "main.yml")
			roleData, err = os.ReadFile(roleTasksPath)
			if err != nil {
				if os.IsNotExist(err) {
					return nil, fmt.Errorf("role tasks file not found: %s", roleTasksPath)
				} else {
					return nil, fmt.Errorf("failed to read role tasks file %s: %w", roleTasksPath, err)
				}
			}
			return roleData, nil
		} else {
			return nil, fmt.Errorf("failed to read role tasks file %s: %w", roleTasksPath, err)
		}
	}
	return roleData, nil
}

// processIncludeRoleDirective handles the 'include_role' directive during preprocessing.
func processIncludeRoleDirective(roleParams interface{}, currentBasePath string, rolesPaths []string) ([]map[string]interface{}, error) {
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

	roleTasksBasePath, err := findRoleInPaths(roleName, rolesPaths, currentBasePath)
	if err != nil {
		return nil, err
	}

	roleData, err := readRoleData(roleTasksBasePath)
	if err != nil {
		return nil, err
	}

	// Recursively preprocess the role's tasks, using the role's tasks directory as the base path
	roleBlocks, err := PreprocessPlaybook(roleData, roleTasksBasePath, rolesPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to preprocess role '%s' tasks from %s: %w", roleName, roleTasksBasePath, err)
	}
	return roleBlocks, nil
}

// processImportRoleDirective handles the 'import_role' directive during preprocessing.
// In this static preprocessing model, it behaves identically to processIncludeRoleDirective.
func processImportRoleDirective(roleParams interface{}, currentBasePath string, rolesPaths []string) ([]map[string]interface{}, error) {
	paramsMap, ok := roleParams.(map[string]interface{})
	if !ok {
		// Handle simple string form: import_role: my_role_name
		if roleNameStr, okStr := roleParams.(string); okStr {
			paramsMap = map[string]interface{}{"name": roleNameStr}
		} else {
			return nil, fmt.Errorf("invalid 'import_role' value: expected map or string, got %T", roleParams)
		}
	}

	roleName, nameOk := paramsMap["name"].(string)
	if !nameOk || roleName == "" {
		return nil, fmt.Errorf("missing or invalid 'name' in import_role directive")
	}

	roleTasksBasePath, err := findRoleInPaths(roleName, rolesPaths, currentBasePath)
	if err != nil {
		return nil, err
	}

	roleData, err := readRoleData(roleTasksBasePath)
	if err != nil {
		return nil, err
	}

	// Recursively preprocess the role's tasks
	roleBlocks, err := PreprocessPlaybook(roleData, roleTasksBasePath, rolesPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to preprocess imported role '%s' tasks from %s: %w", roleName, roleTasksBasePath, err)
	}
	return roleBlocks, nil
}

// processPlaybookRoot handles the root level of an Ansible playbook with 'hosts' field
// and either 'roles' or 'tasks' sections.
func processPlaybookRoot(playbookRoot map[string]interface{}, currentBasePath string, rolesPaths []string) ([]map[string]interface{}, error) {
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
	result = append(result, root_block)

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
			roleBlocks, err := processIncludeRoleDirective(roleParams, currentBasePath, rolesPaths)
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

			// Each task is already a map, so just add it directly to the result
			result = append(result, taskMap)
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

			// Each task is already a map, so just add it directly to the result
			result = append(result, taskMap)
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

			// Each task is already a map, so just add it directly to the result
			result = append(result, taskMap)
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

			// Mark this task as a handler
			handlerMap["is_handler"] = true
			result = append(result, handlerMap)
		}
	}

	return result, nil
}

// preprocessorFunc defines the signature for functions that handle meta directives.
type preprocessorFunc func(value interface{}, basePath string, rolesPaths []string) ([]map[string]interface{}, error)

// preprocessorRegistry maps meta directive keywords to their processing functions.
// Declared here, populated in init() to avoid initialization cycles.
var preprocessorRegistry map[string]preprocessorFunc

// init populates the preprocessorRegistry.
func init() {
	preprocessorRegistry = map[string]preprocessorFunc{
		"include":          processIncludeDirective,
		"include_playbook": processIncludeDirective,
		"import_tasks":     processImportTasksDirective,
		"import_playbook":  processImportTasksDirective,
		"include_role":     processIncludeRoleDirective,
		"import_role":      processImportRoleDirective,
		// Add other meta directives here in the future
	}
}

// preprocessPlaybook takes raw playbook YAML data and a base path,
// recursively processes registered meta directives (include, import_tasks, etc.),
// and returns a flattened list of raw task maps ready for parsing.
func PreprocessPlaybook(data []byte, basePath string, rolesPaths []string) ([]map[string]interface{}, error) {
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
			rootBlocks, err := processPlaybookRoot(block, basePath, rolesPaths)
			if err != nil {
				processErrors = append(processErrors, fmt.Errorf("error processing playbook root: %w", err))
			} else {
				processedBlocks = append(processedBlocks, rootBlocks...)
			}
			continue
		}

		// If not a playbook root, process as before
		processed := false
		for key, value := range block {
			if processor, ok := preprocessorRegistry[key]; ok {
				nestedBlocks, err := processor(value, basePath, rolesPaths)
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
