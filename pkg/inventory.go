package pkg

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/AlexanderGrooff/spage/pkg/plugins"
	"github.com/AlexanderGrooff/spage/pkg/types"
	"gopkg.in/yaml.v3"
)

// decryptVaultValue checks if the provided value is an Ansible vault string and decrypts it.
func decryptVaultValue(value interface{}, vaultPassword string) (interface{}, error) {
	// Only strings (or []byte castable to string) can be vault strings
	var str string
	switch v := value.(type) {
	case string:
		str = v
	case []byte:
		str = string(v)
	default:
		return value, nil
	}

	if !IsAnsibleVaultString(str) {
		return value, nil
	}
	if vaultPassword == "" {
		return value, fmt.Errorf("ansible vault password not provided")
	}

	av, err := NewAnsibleVaultString(str)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ansible vault string: %w", err)
	}
	plain, err := av.Decrypt(vaultPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt ansible vault string: %w", err)
	}
	return plain, nil
}

// decryptMapValues walks a map[string]interface{} and decrypts any vault strings found.
func decryptMapValues(vars map[string]interface{}, vaultPassword string) error {
	if vars == nil {
		return nil
	}
	for k, v := range vars {
		switch typed := v.(type) {
		case string, []byte:
			dec, err := decryptVaultValue(typed, vaultPassword)
			if err != nil {
				return fmt.Errorf("key %q: %w", k, err)
			}
			vars[k] = dec
		case map[string]interface{}:
			if err := decryptMapValues(typed, vaultPassword); err != nil {
				return err
			}
		case []interface{}:
			for i, item := range typed {
				if s, ok := item.(string); ok {
					dec, err := decryptVaultValue(s, vaultPassword)
					if err != nil {
						return fmt.Errorf("key %q[%d]: %w", k, i, err)
					}
					typed[i] = dec
				} else if m, ok := item.(map[string]interface{}); ok {
					if err := decryptMapValues(m, vaultPassword); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// splitInventoryPaths splits a colon-delimited inventory paths string into individual paths
func splitInventoryPaths(inventoryPaths string) []string {
	if inventoryPaths == "" {
		return []string{} // Default to empty list, will fall back to localhost
	}
	paths := strings.Split(inventoryPaths, ":")
	// Filter out empty paths
	var result []string
	for _, path := range paths {
		if strings.TrimSpace(path) != "" {
			result = append(result, strings.TrimSpace(path))
		}
	}
	return result
}

// findInventoryFile searches for inventory files in the provided paths
// It looks for common inventory file names: inventory, inventory.yml, inventory.yaml, hosts
func findInventoryFile(inventoryPaths []string, workingDir string) (string, error) {
	commonNames := []string{"inventory", "inventory.yml", "inventory.yaml", "hosts"}

	for _, inventoryPath := range inventoryPaths {
		var searchPath string
		if filepath.IsAbs(inventoryPath) {
			searchPath = inventoryPath
		} else {
			searchPath = filepath.Join(workingDir, inventoryPath)
		}

		// Check if the path exists
		info, err := os.Stat(searchPath)
		if err != nil {
			// Path doesn't exist, continue to next
			continue
		}

		if info.IsDir() {
			// If the path is a directory, check for common inventory file names
			for _, name := range commonNames {
				fullPath := filepath.Join(searchPath, name)
				if _, err := os.Stat(fullPath); err == nil {
					return fullPath, nil // Return the full path to the file, not the directory
				}
			}
		} else {
			// If the path is a file, return it directly
			return searchPath, nil
		}
	}

	return "", fmt.Errorf("inventory file not found in any of the inventory paths: %v", inventoryPaths)
}

// findAllInventoryFiles searches for all inventory files in the provided paths
// When given directories, it loads ALL files in the directory like Ansible does
func findAllInventoryFiles(inventoryPaths []string, workingDir string) ([]string, error) {
	var foundFiles []string

	for _, inventoryPath := range inventoryPaths {
		var searchPath string
		if filepath.IsAbs(inventoryPath) {
			searchPath = inventoryPath
		} else {
			searchPath = filepath.Join(workingDir, inventoryPath)
		}

		// Check if the path exists
		info, err := os.Stat(searchPath)
		if err != nil {
			// Path doesn't exist, continue to next
			continue
		}

		if info.IsDir() {
			// If the path is a directory, load ALL files in the directory (like Ansible does)
			entries, err := os.ReadDir(searchPath)
			if err != nil {
				common.LogWarn("Failed to read inventory directory", map[string]interface{}{
					"path":  searchPath,
					"error": err.Error(),
				})
				continue
			}

			// Collect all regular files in alphabetical order (like Ansible)
			var dirFiles []string
			for _, entry := range entries {
				if !entry.IsDir() {
					dirFiles = append(dirFiles, entry.Name())
				}
			}

			// Sort files alphabetically as Ansible does
			sort.Strings(dirFiles)

			// Convert to full paths and add to the list
			for _, fileName := range dirFiles {
				fullPath := filepath.Join(searchPath, fileName)
				foundFiles = append(foundFiles, fullPath)
			}
		} else {
			// If the path is a file, add it directly
			foundFiles = append(foundFiles, searchPath)
		}
	}

	if len(foundFiles) == 0 {
		return nil, fmt.Errorf("no inventory files found in any of the inventory paths: %v", inventoryPaths)
	}

	return foundFiles, nil
}

// loadVariablesFromFile loads variables from a YAML file into a map
func loadVariablesFromFile(filePath string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read variable file %s: %w", filePath, err)
	}

	var vars map[string]interface{}
	err = yaml.Unmarshal(data, &vars)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML in variable file %s: %w", filePath, err)
	}

	return vars, nil
}

// loadGroupVars loads variables from group_vars directory structure
// Supports both group_vars/groupname.yml and group_vars/groupname/ directory structures
func loadGroupVars(inventoryDir string, vaultPassword string) (map[string]map[string]interface{}, error) {
	groupVars := make(map[string]map[string]interface{})
	groupVarsDir := filepath.Join(inventoryDir, "group_vars")

	// Check if group_vars directory exists
	if _, err := os.Stat(groupVarsDir); os.IsNotExist(err) {
		return groupVars, nil // No group_vars directory, return empty map
	}

	entries, err := os.ReadDir(groupVarsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read group_vars directory: %w", err)
	}

	for _, entry := range entries {
		var groupName string

		if entry.IsDir() {
			// Handle group_vars/groupname/ directory structure
			groupName = entry.Name()
			if _, exists := groupVars[groupName]; !exists {
				groupVars[groupName] = make(map[string]interface{})
			}

			groupDir := filepath.Join(groupVarsDir, groupName)
			groupFiles, err := os.ReadDir(groupDir)
			if err != nil {
				common.LogWarn("Failed to read group vars directory", map[string]interface{}{
					"group": groupName,
					"path":  groupDir,
					"error": err.Error(),
				})
				continue
			}

			// Load all YAML files in the group directory
			for _, file := range groupFiles {
				if file.IsDir() {
					continue
				}

				fileName := file.Name()
				if !strings.HasSuffix(fileName, ".yml") && !strings.HasSuffix(fileName, ".yaml") {
					continue
				}

				filePath := filepath.Join(groupDir, fileName)
				fileVars, err := loadVariablesFromFile(filePath)
				if err != nil {
					common.LogWarn("Failed to load group vars file", map[string]interface{}{
						"group": groupName,
						"file":  filePath,
						"error": err.Error(),
					})
					continue
				}

				// Decrypt any vault values
				if vaultPassword != "" {
					if err := decryptMapValues(fileVars, vaultPassword); err != nil {
						common.LogWarn("Failed to decrypt group vars", map[string]interface{}{
							"group": groupName,
							"file":  filePath,
							"error": err.Error(),
						})
						continue
					}
				}

				// Merge file variables into group variables
				for k, v := range fileVars {
					groupVars[groupName][k] = v
				}
			}
		} else {
			// Handle group_vars/groupname.yml file structure
			fileName := entry.Name()
			if !strings.HasSuffix(fileName, ".yml") && !strings.HasSuffix(fileName, ".yaml") {
				continue
			}

			// Extract group name from filename (remove .yml/.yaml extension)
			groupName = strings.TrimSuffix(strings.TrimSuffix(fileName, ".yml"), ".yaml")
			if _, exists := groupVars[groupName]; !exists {
				groupVars[groupName] = make(map[string]interface{})
			}

			filePath := filepath.Join(groupVarsDir, fileName)
			fileVars, err := loadVariablesFromFile(filePath)
			if err != nil {
				common.LogWarn("Failed to load group vars file", map[string]interface{}{
					"group": groupName,
					"file":  filePath,
					"error": err.Error(),
				})
				continue
			}

			// Decrypt any vault values
			if vaultPassword != "" {
				if err := decryptMapValues(fileVars, vaultPassword); err != nil {
					common.LogWarn("Failed to decrypt group vars", map[string]interface{}{
						"group": groupName,
						"file":  filePath,
						"error": err.Error(),
					})
					continue
				}
			}
			for k, v := range fileVars {
				groupVars[groupName][k] = v
			}
			common.LogDebug("Loaded group variables", map[string]interface{}{
				"group":      groupName,
				"vars_count": len(fileVars),
			})
		}
	}

	return groupVars, nil
}

// loadHostVars loads variables from host_vars directory structure
// Supports both host_vars/hostname.yml and host_vars/hostname/ directory structures
func loadHostVars(inventoryDir string, vaultPassword string) (map[string]map[string]interface{}, error) {
	hostVars := make(map[string]map[string]interface{})
	hostVarsDir := filepath.Join(inventoryDir, "host_vars")

	// Check if host_vars directory exists
	if _, err := os.Stat(hostVarsDir); os.IsNotExist(err) {
		return hostVars, nil // No host_vars directory, return empty map
	}

	entries, err := os.ReadDir(hostVarsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read host_vars directory: %w", err)
	}

	for _, entry := range entries {
		var hostName string

		if entry.IsDir() {
			// Handle host_vars/hostname/ directory structure
			hostName = entry.Name()
			if hostVars[hostName] == nil {
				hostVars[hostName] = make(map[string]interface{})
			}

			hostDir := filepath.Join(hostVarsDir, hostName)
			hostFiles, err := os.ReadDir(hostDir)
			if err != nil {
				common.LogWarn("Failed to read host vars directory", map[string]interface{}{
					"host":  hostName,
					"path":  hostDir,
					"error": err.Error(),
				})
				continue
			}

			// Load all YAML files in the host directory
			for _, file := range hostFiles {
				if file.IsDir() {
					continue
				}

				fileName := file.Name()
				if !strings.HasSuffix(fileName, ".yml") && !strings.HasSuffix(fileName, ".yaml") {
					continue
				}

				filePath := filepath.Join(hostDir, fileName)
				fileVars, err := loadVariablesFromFile(filePath)
				if err != nil {
					common.LogWarn("Failed to load host vars file", map[string]interface{}{
						"host":  hostName,
						"file":  filePath,
						"error": err.Error(),
					})
					continue
				}

				// Decrypt any vault values
				if vaultPassword != "" {
					if err := decryptMapValues(fileVars, vaultPassword); err != nil {
						common.LogWarn("Failed to decrypt host vars", map[string]interface{}{
							"host":  hostName,
							"file":  filePath,
							"error": err.Error(),
						})
						continue
					}
				}

				// Merge file variables into host variables
				for k, v := range fileVars {
					hostVars[hostName][k] = v
				}
			}
		} else {
			// Handle host_vars/hostname.yml file structure
			fileName := entry.Name()
			if !strings.HasSuffix(fileName, ".yml") && !strings.HasSuffix(fileName, ".yaml") {
				continue
			}

			// Extract host name from filename (remove .yml/.yaml extension)
			hostName = strings.TrimSuffix(strings.TrimSuffix(fileName, ".yml"), ".yaml")
			if hostVars[hostName] == nil {
				hostVars[hostName] = make(map[string]interface{})
			}

			filePath := filepath.Join(hostVarsDir, fileName)
			fileVars, err := loadVariablesFromFile(filePath)
			if err != nil {
				common.LogWarn("Failed to load host vars file", map[string]interface{}{
					"host":  hostName,
					"file":  filePath,
					"error": err.Error(),
				})
				continue
			}

			// Decrypt any vault values
			if vaultPassword != "" {
				if err := decryptMapValues(fileVars, vaultPassword); err != nil {
					common.LogWarn("Failed to decrypt host vars", map[string]interface{}{
						"host":  hostName,
						"file":  filePath,
						"error": err.Error(),
					})
					continue
				}
			}
			for k, v := range fileVars {
				hostVars[hostName][k] = v
			}
		}

		common.LogDebug("Loaded host variables", map[string]interface{}{
			"host":       hostName,
			"vars_count": len(hostVars[hostName]),
		})
	}

	return hostVars, nil
}

// mergeInventories merges multiple inventories into a single inventory
// Later inventories take precedence over earlier ones for conflicting keys
func mergeInventories(inventories []*Inventory) *Inventory {
	if len(inventories) == 0 {
		return &Inventory{
			Hosts:  make(map[string]*Host),
			Vars:   make(map[string]interface{}),
			Groups: make(map[string]*Group),
		}
	}

	if len(inventories) == 1 {
		return inventories[0]
	}

	merged := &Inventory{
		Hosts:  make(map[string]*Host),
		Vars:   make(map[string]interface{}),
		Groups: make(map[string]*Group),
	}

	// Merge in order, later inventories override earlier ones
	for _, inv := range inventories {
		// Merge global vars
		for k, v := range inv.Vars {
			merged.Vars[k] = v
		}

		// Merge groups
		for groupName, group := range inv.Groups {
			if existingGroup, exists := merged.Groups[groupName]; exists {
				// Merge group vars
				if existingGroup.Vars == nil {
					existingGroup.Vars = make(map[string]interface{})
				}
				for k, v := range group.Vars {
					existingGroup.Vars[k] = v
				}

				// Merge group hosts
				if existingGroup.Hosts == nil {
					existingGroup.Hosts = make(map[string]*Host)
				}
				for hostName, host := range group.Hosts {
					existingGroup.Hosts[hostName] = host
				}
			} else {
				// Create new group
				newGroup := &Group{
					Vars:  make(map[string]interface{}),
					Hosts: make(map[string]*Host),
				}

				// Copy vars
				for k, v := range group.Vars {
					newGroup.Vars[k] = v
				}

				// Copy hosts
				for hostName, host := range group.Hosts {
					newGroup.Hosts[hostName] = host
				}

				merged.Groups[groupName] = newGroup
			}
		}

		// Merge hosts
		for hostName, host := range inv.Hosts {
			if existingHost, exists := merged.Hosts[hostName]; exists {
				// Merge host vars - later inventory takes precedence
				if existingHost.Vars == nil {
					existingHost.Vars = make(map[string]interface{})
				}
				for k, v := range host.Vars {
					existingHost.Vars[k] = v
				}

				// Update other host fields if they're set in the newer inventory
				if host.Host != "" {
					existingHost.Host = host.Host
				}

				// Merge groups
				if existingHost.Groups == nil {
					existingHost.Groups = make(map[string]string)
				}
				for k, v := range host.Groups {
					existingHost.Groups[k] = v
				}

				// Update IsLocal flag
				existingHost.IsLocal = host.IsLocal
			} else {
				// Copy the host completely
				newHost := &Host{
					Name:    host.Name,
					Host:    host.Host,
					Vars:    make(map[string]interface{}),
					Groups:  make(map[string]string),
					IsLocal: host.IsLocal,
				}

				// Copy vars
				for k, v := range host.Vars {
					newHost.Vars[k] = v
				}

				// Copy groups
				for k, v := range host.Groups {
					newHost.Groups[k] = v
				}

				merged.Hosts[hostName] = newHost
			}
		}
	}

	return merged
}

// Use types from shared package
type Inventory = types.Inventory
type Host = types.Host
type Group = types.Group

func LoadInventory(path string, cfg *config.Config) (*Inventory, error) {
	return LoadInventoryWithPaths(path, "", ".", "", cfg)
}

func LoadInventoryWithLimit(path string, limitPattern string, cfg *config.Config) (*Inventory, error) {
	if limitPattern == "" {
		limitPattern = cfg.Limit
	}
	return LoadInventoryWithPaths(path, "", ".", limitPattern, cfg)
}

func LoadInventoryWithPaths(path string, inventoryPaths string, workingDir string, limitPattern string, cfg *config.Config) (*Inventory, error) {
	var filesToLoad []string

	var vaultPassword string
	if cfg == nil {
		vaultPassword = ""
	} else {
		vaultPassword = cfg.AnsibleVaultPassword
	}

	if path != "" {
		// Explicit path provided, use it directly
		filesToLoad = []string{path}
	} else if inventoryPaths != "" {
		// No explicit path but inventory paths configured, search for inventory files
		paths := splitInventoryPaths(inventoryPaths)
		if len(paths) > 0 {
			foundFiles, err := findAllInventoryFiles(paths, workingDir)
			if err != nil {
				// If no inventory found in paths, fall back to localhost
				common.LogDebug("No inventory files found in configured paths, using localhost", map[string]interface{}{
					"inventoryPaths": inventoryPaths,
					"searchPaths":    paths,
				})
			} else {
				filesToLoad = foundFiles
				common.LogDebug("Found multiple inventory files", map[string]interface{}{
					"files": foundFiles,
				})
			}
		}
	}

	if len(filesToLoad) == 0 {
		common.LogDebug("No inventory file specified, assuming target is this machine", nil)
		defaultInventory := &Inventory{
			Hosts: map[string]*Host{
				"localhost": {Name: "localhost", IsLocal: true, Host: "localhost"},
			},
		}

		// Apply host limiting to default inventory if specified
		if limitPattern != "" {
			common.LogInfo("Applying host limit pattern to default inventory", map[string]interface{}{
				"pattern": limitPattern,
			})
			defaultInventory = filterInventoryByLimit(defaultInventory, limitPattern)
			if len(defaultInventory.Hosts) == 0 {
				return nil, fmt.Errorf("no hosts matched limit pattern: %s", limitPattern)
			}
			common.LogInfo("Filtered default inventory", map[string]interface{}{
				"matched_hosts": len(defaultInventory.Hosts),
			})
		}

		return defaultInventory, nil
	}

	var inventories []*Inventory

	// Initialize plugin manager
	pm := plugins.NewPluginManager()

	// Load all inventory files
	for _, filePath := range filesToLoad {
		common.LogDebug("Loading inventory file", map[string]interface{}{
			"file": filePath,
		})

		// If the file is executable, execute it with --list to obtain JSON inventory
		if info, err := os.Stat(filePath); err == nil {
			if !info.IsDir() && (info.Mode()&0111) != 0 { // any execute bit set
				common.LogDebug("Detected executable inventory file, executing", map[string]interface{}{
					"file": filePath,
				})
				execInv, err := pm.LoadInventoryFromExecutable(context.Background(), filePath, "--list")
				if err != nil {
					return nil, fmt.Errorf("failed to load inventory from executable %s: %w", filePath, err)
				}
				inventories = append(inventories, execInv)
				continue
			}
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			// Change from log.Fatalf to return error to allow fallback behavior
			common.LogWarn("Failed to read inventory file, skipping", map[string]interface{}{
				"file":  filePath,
				"error": err.Error(),
			})
			continue
		}

		// Check if this is a plugin-based inventory
		var pluginConfig map[string]interface{}
		if err := yaml.Unmarshal(data, &pluginConfig); err == nil {
			if pluginName, hasPlugin := pluginConfig["plugin"]; hasPlugin {
				common.LogDebug("Detected plugin-based inventory", map[string]interface{}{
					"file":   filePath,
					"plugin": pluginName,
				})

				// Hint plugin loader about the working directory of this inventory file
				if pluginConfig == nil {
					pluginConfig = make(map[string]interface{})
				}
				pluginConfig["__spage_cwd"] = filepath.Dir(filePath)
				// Pass through inventory plugin settings from spage config
				if cfg != nil {
					if cfg.InventoryPlugins != "" {
						pluginConfig["__spage_inventory_plugins"] = cfg.InventoryPlugins
					}
					if len(cfg.EnablePlugins) > 0 {
						pluginConfig["__spage_enable_plugins"] = cfg.EnablePlugins
					}
				}
				pluginInventory, err := pm.LoadInventoryFromPlugin(context.Background(), fmt.Sprintf("%v", pluginName), pluginConfig)
				if err != nil {
					return nil, fmt.Errorf("failed to load inventory from plugin %s: %w", pluginName, err)
				}

				// Convert plugin inventory to standard inventory format
				inventory := pluginInventory
				inventory.Plugin = fmt.Sprintf("%v", pluginName)
				inventories = append(inventories, inventory)
				continue
			}
		}

		// Regular static inventory file
		var inventory Inventory
		inventory.Hosts = make(map[string]*Host)
		inventory.Groups = make(map[string]*Group)
		err = yaml.Unmarshal(data, &inventory)
		if err != nil {
			return nil, fmt.Errorf("error parsing inventory file %s: %w", filePath, err)
		}

		// Process the loaded inventory
		for name, host := range inventory.Hosts {
			common.DebugOutput("Adding host %q to inventory from file %s", name, filePath)

			// Decrypt host variables if vault password is provided
			if vaultPassword != "" && host.Vars != nil {
				if err := decryptMapValues(host.Vars, vaultPassword); err != nil {
					common.LogWarn("Failed to decrypt host variables", map[string]interface{}{
						"host":  name,
						"file":  filePath,
						"error": err.Error(),
					})
				}
			}

			host.Prepare()
			host.Name = name

			if host.Host == "localhost" || host.Host == "" {
				host.IsLocal = true
			}
			inventory.Hosts[name] = host
		}

		for groupName, group := range inventory.Groups {
			// Decrypt group variables if vault password is provided
			if vaultPassword != "" && group.Vars != nil {
				if err := decryptMapValues(group.Vars, vaultPassword); err != nil {
					common.LogWarn("Failed to decrypt group variables", map[string]interface{}{
						"group": groupName,
						"file":  filePath,
						"error": err.Error(),
					})
				}
			}

			for name, host := range group.Hosts {
				common.DebugOutput("Found host %q in group %q from file %s", name, groupName, filePath)
				var h *Host
				if h = inventory.Hosts[name]; h != nil {
					common.DebugOutput("Host %q already in inventory", name)
					// Merge variables from the new host instance into the existing host
					if host.Vars != nil {
						// Decrypt host variables from group if vault password is provided
						if vaultPassword != "" {
							if err := decryptMapValues(host.Vars, vaultPassword); err != nil {
								common.LogWarn("Failed to decrypt host variables from group", map[string]interface{}{
									"host":  name,
									"group": groupName,
									"file":  filePath,
									"error": err.Error(),
								})
							}
						}

						if h.Vars == nil {
							h.Vars = make(map[string]interface{})
						}
						for k, v := range host.Vars {
							h.Vars[k] = v
						}
					}
				} else {
					h = host
				}
				h.Prepare()
				if h.Host == "" {
					h.Host = host.Host
				}
				if h.Name == "" {
					h.Name = name
				}

				if h.Host == "localhost" || h.Host == "" {
					h.IsLocal = true
				}

				common.DebugOutput("Adding host %q to inventory from group %q", name, groupName)

				inventory.Hosts[name] = h
				for k, v := range group.Vars {
					inventory.Hosts[name].Vars[k] = v
				}
			}
		}

		// Decrypt top-level inventory vars before propagating to hosts
		if vaultPassword != "" {
			if err := decryptMapValues(inventory.Vars, vaultPassword); err != nil {
				common.LogWarn("Failed to decrypt top-level inventory vars", map[string]interface{}{
					"file":  filePath,
					"error": err.Error(),
				})
			}
		}
		for k, v := range inventory.Vars {
			for _, host := range inventory.Hosts {
				host.Vars[k] = v
			}
		}

		inventories = append(inventories, &inventory)
	}

	// Merge all inventories into one
	mergedInventory := mergeInventories(inventories)

	// Load group_vars and host_vars from directories adjacent to inventory files
	// We'll use the directory of the first inventory file as the base directory
	// Get absolute path to inventory directory to ensure proper group_vars/host_vars resolution
	inventoryPath, err := filepath.Abs(filesToLoad[0])
	if err != nil {
		common.LogWarn("Failed to get absolute path for inventory file", map[string]interface{}{
			"file":  filesToLoad[0],
			"error": err.Error(),
		})
		inventoryPath = filesToLoad[0] // Fallback to relative path
	}
	inventoryDir := filepath.Dir(inventoryPath)

	// Load group variables - try both inventory directory and playbooks subdirectory
	var groupVarsDir string
	playbooksDir := filepath.Join(inventoryDir, "playbooks")
	if _, err := os.Stat(filepath.Join(playbooksDir, "group_vars")); err == nil {
		// group_vars directory exists in playbooks subdirectory
		groupVarsDir = playbooksDir
	} else {
		// Fall back to inventory directory
		groupVarsDir = inventoryDir
	}

	// Also check if group_vars exists directly in the inventory directory
	if _, err := os.Stat(filepath.Join(inventoryDir, "group_vars")); err == nil {
		groupVarsDir = inventoryDir
	}

	common.LogDebug("Attempting to load group_vars", map[string]interface{}{
		"inventory_dir":  inventoryDir,
		"group_vars_dir": groupVarsDir,
	})

	groupVars, err := loadGroupVars(groupVarsDir, vaultPassword)
	if err != nil {
		common.LogWarn("Failed to load group variables", map[string]interface{}{
			"directory": inventoryDir,
			"error":     err.Error(),
		})
	} else if len(groupVars) > 0 {
		// Merge 'all' group vars into the existing 'all' group (no host-by-host special-casing)
		if allGroupVars, exists := groupVars["all"]; exists {
			// Decrypt 'all' group variables if vault password is provided
			if vaultPassword != "" {
				if err := decryptMapValues(allGroupVars, vaultPassword); err != nil {
					common.LogWarn("Failed to decrypt 'all' group variables", map[string]interface{}{
						"error": err.Error(),
					})
				}
			}

			if allGroup, groupExists := mergedInventory.Groups["all"]; groupExists {
				if allGroup.Vars == nil {
					allGroup.Vars = make(map[string]interface{})
				}
				maps.Copy(allGroup.Vars, allGroupVars)
			} else {
				// Create the 'all' group with current hosts; Vars from group_vars
				allHosts := make(map[string]*Host)
				for hostName, host := range mergedInventory.Hosts {
					allHosts[hostName] = host
				}
				mergedInventory.Groups["all"] = &Group{
					Hosts: allHosts,
					Vars:  allGroupVars,
				}
			}
		}

		// Decrypt group variables if vault password is provided
		if vaultPassword != "" {
			for groupName, vars := range groupVars {
				if groupName == "all" {
					continue
				}
				if err := decryptMapValues(vars, vaultPassword); err != nil {
					common.LogWarn("Failed to decrypt group variables", map[string]interface{}{
						"group": groupName,
						"error": err.Error(),
					})
				}
			}
		}

		// Apply group variables to existing groups (excluding 'all' which is handled above)
		for groupName, vars := range groupVars {
			if groupName == "all" {
				continue
			}
			if group, exists := mergedInventory.Groups[groupName]; exists {
				if group.Vars == nil {
					group.Vars = make(map[string]interface{})
				}
				maps.Copy(group.Vars, vars)
			} else {
				mergedInventory.Groups[groupName] = &Group{
					Hosts: make(map[string]*Host),
					Vars:  vars,
				}
			}
		}
	}

	// Load host variables using same directory resolution as group_vars
	// We'll use the same directory resolution logic as group_vars
	var hostVarsDir string
	if _, err := os.Stat(filepath.Join(playbooksDir, "host_vars")); err == nil {
		// host_vars directory exists in playbooks subdirectory
		hostVarsDir = playbooksDir
	} else {
		// Fall back to inventory directory
		hostVarsDir = inventoryDir
	}

	// Also check if host_vars exists directly in the inventory directory
	if _, err := os.Stat(filepath.Join(inventoryDir, "host_vars")); err == nil {
		hostVarsDir = inventoryDir
	}

	common.LogDebug("Attempting to load host_vars", map[string]interface{}{
		"inventory_dir": inventoryDir,
		"host_vars_dir": hostVarsDir,
	})

	hostVars, err := loadHostVars(hostVarsDir, vaultPassword)
	if err != nil {
		common.LogWarn("Failed to load host variables", map[string]interface{}{
			"directory": inventoryDir,
			"error":     err.Error(),
		})
	} else if len(hostVars) > 0 {
		// Decrypt host variables if vault password is provided
		if vaultPassword != "" {
			for hostName, vars := range hostVars {
				if err := decryptMapValues(vars, vaultPassword); err != nil {
					common.LogWarn("Failed to decrypt host variables", map[string]interface{}{
						"host":  hostName,
						"error": err.Error(),
					})
				}
			}
		}

		// Apply host variables only to existing hosts (align with Ansible behavior)
		for hostName, vars := range hostVars {
			if host, exists := mergedInventory.Hosts[hostName]; exists {
				// Host already exists, merge variables (host_vars take precedence)
				if host.Vars == nil {
					host.Vars = make(map[string]interface{})
				}
				maps.Copy(host.Vars, vars)
				common.LogDebug("Applied host_vars to existing host", map[string]interface{}{
					"host":       hostName,
					"vars_count": len(vars),
				})
			} else {
				// Do not create new hosts from host_vars
				common.LogDebug("Ignoring host_vars for unknown host (no creation)", map[string]interface{}{
					"host":       hostName,
					"vars_count": len(vars),
				})
			}
		}
	}

	// Apply host limiting if specified
	if limitPattern != "" {
		mergedInventory = filterInventoryByLimit(mergedInventory, limitPattern)
		if len(mergedInventory.Hosts) == 0 {
			return nil, fmt.Errorf("no hosts matched limit pattern: %s", limitPattern)
		}
	}

	// Ensure 'all' group contains all hosts, and all hosts have 'all' group
	if mergedInventory.Groups == nil {
		mergedInventory.Groups = make(map[string]*Group)
	}
	if allGroup, exists := mergedInventory.Groups["all"]; exists {
		if allGroup.Hosts == nil {
			allGroup.Hosts = make(map[string]*Host)
		}
		for hostName, host := range mergedInventory.Hosts {
			allGroup.Hosts[hostName] = host
		}
	} else {
		allHosts := make(map[string]*Host)
		for hostName, host := range mergedInventory.Hosts {
			allHosts[hostName] = host
		}
		allGroup = &Group{
			Hosts: allHosts,
			Vars:  make(map[string]interface{}),
		}
		mergedInventory.Groups["all"] = allGroup
	}

	// Gather facts from all sources and put them in the host.Vars map
	for _, host := range mergedInventory.Hosts {
		host.Groups["all"] = "all"
		host.Vars = mergedInventory.GetInitialFactsForHost(host)
	}

	return mergedInventory, nil
}

// GetContextForHost creates a host context from inventory and host data
func GetContextForHost(inventory *Inventory, host *Host, cfg *config.Config) (*HostContext, error) {
	ctx, err := InitializeHostContext(host, cfg)
	if err != nil {
		return nil, err
	}

	for k, v := range cfg.Facts {
		ctx.Facts.Store(k, v)
	}

	for k, v := range host.Vars {
		ctx.Facts.Store(k, v)
	}

	return ctx, nil
}

// filterInventoryByLimit filters hosts in inventory based on the limit pattern
// Supports patterns like: hostname, group_name, hostname1:hostname2, *pattern, group:hostname
func filterInventoryByLimit(inventory *Inventory, limitPattern string) *Inventory {
	if limitPattern == "" {
		return inventory
	}

	common.LogDebug("Filtering inventory with limit pattern", map[string]interface{}{
		"pattern": limitPattern,
	})

	filteredInventory := &Inventory{
		Hosts:  make(map[string]*Host),
		Groups: make(map[string]*Group),
		Vars:   inventory.Vars,
	}

	// Split patterns by comma (like Ansible does for -l flag)
	patterns := strings.Split(limitPattern, ",")
	matchedHosts := make(map[string]bool)

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}

		// Check if pattern matches any hosts directly
		for hostName, host := range inventory.Hosts {
			if matchesHostPattern(hostName, pattern) {
				matchedHosts[hostName] = true
				filteredInventory.Hosts[hostName] = host
			}
		}

		// Check if pattern matches any groups
		for groupName, group := range inventory.Groups {
			if matchesHostPattern(groupName, pattern) {
				// Add all hosts in the matching group
				for hostName, host := range group.Hosts {
					if !matchedHosts[hostName] {
						matchedHosts[hostName] = true
						filteredInventory.Hosts[hostName] = host
					}
				}
			}
		}
	}

	// Copy all groups with their variables, but filter hosts within groups
	for groupName, group := range inventory.Groups {
		filteredGroup := &Group{
			Vars:  group.Vars,
			Hosts: make(map[string]*Host),
		}

		// Only include hosts that were matched
		for hostName, host := range group.Hosts {
			if matchedHosts[hostName] {
				filteredGroup.Hosts[hostName] = host
			}
		}

		// Preserve all groups, even if they have no matched hosts
		filteredInventory.Groups[groupName] = filteredGroup
	}

	common.LogDebug("Filtered inventory", map[string]interface{}{
		"original_hosts": len(inventory.Hosts),
		"filtered_hosts": len(filteredInventory.Hosts),
	})

	return filteredInventory
}

// matchesHostPattern checks if a hostname matches a pattern
// Supports glob-style patterns (* and ?)
func matchesHostPattern(hostname, pattern string) bool {
	// Direct match
	if hostname == pattern {
		return true
	}

	// Convert glob pattern to regex
	if strings.Contains(pattern, "*") || strings.Contains(pattern, "?") {
		// Escape special regex characters except * and ?
		regexPattern := regexp.QuoteMeta(pattern)
		// Convert glob wildcards to regex
		regexPattern = strings.ReplaceAll(regexPattern, "\\*", ".*")
		regexPattern = strings.ReplaceAll(regexPattern, "\\?", ".")
		// Anchor the pattern
		regexPattern = "^" + regexPattern + "$"

		matched, err := regexp.MatchString(regexPattern, hostname)
		if err != nil {
			common.LogWarn("Invalid regex pattern", map[string]interface{}{
				"pattern": pattern,
				"error":   err.Error(),
			})
			return false
		}
		return matched
	}

	return false
}

func GetContextForRun(inventory *Inventory, graph *Graph, cfg *config.Config) (map[string]*HostContext, error) {
	var err error
	contexts := make(map[string]*HostContext)
	for _, host := range inventory.Hosts {
		// Honor connection overrides before initializing host context
		// 1) CLI/config override
		if cfg != nil && strings.EqualFold(cfg.Connection, "local") {
			host.IsLocal = true
		} else {
			// 2) Play-level connection from graph vars (preprocessed as ansible_connection)
			if graph != nil && graph.Vars != nil {
				if connVal, ok := graph.Vars["ansible_connection"]; ok {
					if connStr, ok := connVal.(string); ok && strings.EqualFold(connStr, "local") {
						host.IsLocal = true
					}
				}
			}
		}
		common.DebugOutput("Getting context for host %q", host.Name)
		contexts[host.Name], err = GetContextForHost(inventory, host, cfg)
		if err != nil {
			return nil, fmt.Errorf("could not get context for host '%s' (%s): %w", host.Name, host.Host, err)
		}

		// Initialize the HandlerTracker with handlers from the graph
		contexts[host.Name].InitializeHandlerTracker(graph.Handlers)

		// Add graph vars to host context
		for k, v := range graph.Vars {
			contexts[host.Name].Facts.Store(k, v)
		}

		// Add global facts from config to host context
		if cfg != nil && cfg.Facts != nil {
			for k, v := range cfg.Facts {
				contexts[host.Name].Facts.Store(k, v)
			}
		}
	}
	return contexts, nil
}
