package pkg

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"

	"gopkg.in/yaml.v3"
)

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
func loadGroupVars(inventoryDir string) (map[string]map[string]interface{}, error) {
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
func loadHostVars(inventoryDir string) (map[string]map[string]interface{}, error) {
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
		var varsToLoad map[string]interface{}

		if entry.IsDir() {
			// Handle host_vars/hostname/ directory structure
			hostName = entry.Name()
			varsToLoad = make(map[string]interface{})

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

				// Merge file variables into host variables
				for k, v := range fileVars {
					varsToLoad[k] = v
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
			varsToLoad = fileVars
		}

		if len(varsToLoad) > 0 {
			hostVars[hostName] = varsToLoad
			common.LogDebug("Loaded host variables", map[string]interface{}{
				"host":       hostName,
				"vars_count": len(varsToLoad),
			})
		}
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

type Inventory struct {
	Hosts  map[string]*Host       `yaml:"all"`
	Vars   map[string]interface{} `yaml:"vars"`
	Groups map[string]*Group      `yaml:"-"` // Groups are parsed manually from top-level keys
}

// UnmarshalYAML implements custom YAML unmarshaling for Inventory to handle top-level groups
func (inv *Inventory) UnmarshalYAML(value *yaml.Node) error {
	// Create a map to capture all YAML data
	var rawData map[string]interface{}
	if err := value.Decode(&rawData); err != nil {
		return err
	}

	// Initialize maps
	inv.Hosts = make(map[string]*Host)
	inv.Vars = make(map[string]interface{})
	inv.Groups = make(map[string]*Group)

	// Process each top-level key
	for key, val := range rawData {
		switch key {
		case "vars":
			// Handle global variables
			if varsData, ok := val.(map[string]interface{}); ok {
				inv.Vars = varsData
			}
		default:
			// All top-level keys (including "all") are groups with hosts and vars subkeys
			if groupData, ok := val.(map[string]interface{}); ok {
				var group Group
				// Convert groupData to YAML bytes and unmarshal into Group
				groupBytes, err := yaml.Marshal(groupData)
				if err != nil {
					return fmt.Errorf("failed to marshal group data for %s: %w", key, err)
				}
				if err := yaml.Unmarshal(groupBytes, &group); err != nil {
					return fmt.Errorf("failed to unmarshal group %s: %w", key, err)
				}
				// Ensure hosts map is initialized
				if group.Hosts == nil {
					group.Hosts = make(map[string]*Host)
				}

				// For the "all" group, also add hosts to the inventory's main Hosts map
				if key == "all" {
					for hostName, host := range group.Hosts {
						host.Name = hostName
						inv.Hosts[hostName] = host
					}
				}

				inv.Groups[key] = &group
			}
		}
	}

	return nil
}

type Host struct {
	Name    string `json:"name"`
	Host    string `yaml:"host"`
	Vars    map[string]interface{}
	Groups  map[string]string `yaml:"groups"`
	IsLocal bool
	Config  *config.Config // Store config for SSH connections
}

func (h Host) String() string {
	return h.Name
}

// UnmarshalYAML implements custom YAML unmarshaling for Host to capture unknown fields into Vars
func (h *Host) UnmarshalYAML(value *yaml.Node) error {
	// Create a map to capture all YAML data
	var rawData map[string]interface{}
	if err := value.Decode(&rawData); err != nil {
		return err
	}

	// Initialize Vars map if nil
	if h.Vars == nil {
		h.Vars = make(map[string]interface{})
	}

	// Process known fields
	if host, ok := rawData["host"]; ok {
		if hostStr, ok := host.(string); ok {
			h.Host = hostStr
		}
	}

	if groups, ok := rawData["groups"]; ok {
		if groupsMap, ok := groups.(map[string]interface{}); ok {
			if h.Groups == nil {
				h.Groups = make(map[string]string)
			}
			for k, v := range groupsMap {
				if vStr, ok := v.(string); ok {
					h.Groups[k] = vStr
				}
			}
		}
	}

	// Put all other fields into Vars (including ansible_ssh_private_key_file)
	knownFields := map[string]bool{
		"host":   true,
		"groups": true,
	}

	for key, value := range rawData {
		if !knownFields[key] {
			h.Vars[key] = value
		}
	}

	common.LogDebug("Processed host YAML data", map[string]interface{}{
		"host":    h.Host,
		"vars":    h.Vars,
		"rawData": rawData,
	})

	return nil
}

func (h *Host) Prepare() {
	if h.Vars == nil {
		h.Vars = make(map[string]interface{})
	}
	if h.Groups == nil {
		h.Groups = make(map[string]string)
	}
}

type Group struct {
	Hosts map[string]*Host       `yaml:"hosts"`
	Vars  map[string]interface{} `yaml:"vars"`
}

func LoadInventory(path string) (*Inventory, error) {
	return LoadInventoryWithPaths(path, "", ".")
}

func LoadInventoryWithPaths(path string, inventoryPaths string, workingDir string) (*Inventory, error) {
	var filesToLoad []string

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
		return &Inventory{
			Hosts: map[string]*Host{
				"localhost": {Name: "localhost", IsLocal: true, Host: "localhost"},
			},
		}, nil
	}

	var inventories []*Inventory

	// Load all inventory files
	for _, filePath := range filesToLoad {
		common.LogDebug("Loading inventory file", map[string]interface{}{
			"file": filePath,
		})

		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Fatalf("Error reading YAML file %s: %v", filePath, err)
		}

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
			host.Prepare()
			host.Name = name

			if host.Host == "localhost" || host.Host == "" {
				host.IsLocal = true
			}
			inventory.Hosts[name] = host
		}

		for groupName, group := range inventory.Groups {
			for name, host := range group.Hosts {
				common.DebugOutput("Found host %q in group %q from file %s", name, groupName, filePath)
				var h *Host
				if h = inventory.Hosts[name]; h != nil {
					common.DebugOutput("Host %q already in inventory", name)
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
	if len(filesToLoad) > 0 {
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

		common.LogDebug("Attempting to load group_vars", map[string]interface{}{
			"inventory_dir":  inventoryDir,
			"group_vars_dir": groupVarsDir,
		})
		groupVars, err := loadGroupVars(groupVarsDir)
		if err != nil {
			common.LogWarn("Failed to load group variables", map[string]interface{}{
				"directory": inventoryDir,
				"error":     err.Error(),
			})
		} else {
			var groupNames []string
			for k := range groupVars {
				groupNames = append(groupNames, k)
			}
			common.LogDebug("Loaded group_vars", map[string]interface{}{
				"directory":   inventoryDir,
				"group_count": len(groupVars),
				"groups":      groupNames,
			})
			if len(groupVars) > 0 {
				// Handle the special 'all' group first - applies to all hosts like in Ansible
				if allGroupVars, exists := groupVars["all"]; exists {
					// Apply 'all' group variables to all hosts
					for _, host := range mergedInventory.Hosts {
						if host.Vars == nil {
							host.Vars = make(map[string]interface{})
						}
						for k, v := range allGroupVars {
							host.Vars[k] = v
						}
					}

					// Also create/update the 'all' group in the inventory
					if allGroup, groupExists := mergedInventory.Groups["all"]; groupExists {
						// Group already exists, merge variables
						if allGroup.Vars == nil {
							allGroup.Vars = make(map[string]interface{})
						}
						for k, v := range allGroupVars {
							allGroup.Vars[k] = v
						}
					} else {
						// Create the 'all' group with all hosts
						allHosts := make(map[string]*Host)
						for hostName, host := range mergedInventory.Hosts {
							allHosts[hostName] = host
						}
						mergedInventory.Groups["all"] = &Group{
							Hosts: allHosts,
							Vars:  allGroupVars,
						}
					}

					common.LogDebug("Applied 'all' group_vars to all hosts (Ansible-style)", map[string]interface{}{
						"vars_count": len(allGroupVars),
						"host_count": len(mergedInventory.Hosts),
					})
				}

				// Apply group variables to existing groups (excluding 'all' which was handled above)
				for groupName, vars := range groupVars {
					if groupName == "all" {
						continue // Already handled above
					}

					if group, exists := mergedInventory.Groups[groupName]; exists {
						// Group already exists, merge variables
						if group.Vars == nil {
							group.Vars = make(map[string]interface{})
						}
						for k, v := range vars {
							group.Vars[k] = v
						}
						common.LogDebug("Applied group_vars to existing group", map[string]interface{}{
							"group":      groupName,
							"vars_count": len(vars),
						})
					} else {
						// Create new group with variables
						mergedInventory.Groups[groupName] = &Group{
							Hosts: make(map[string]*Host),
							Vars:  vars,
						}
						common.LogDebug("Created new group from group_vars", map[string]interface{}{
							"group":      groupName,
							"vars_count": len(vars),
						})
					}
				}
			}

			// Load host variables
			hostVars, err := loadHostVars(inventoryDir)
			if err != nil {
				common.LogWarn("Failed to load host variables", map[string]interface{}{
					"directory": inventoryDir,
					"error":     err.Error(),
				})
			} else if len(hostVars) > 0 {
				// Apply host variables to existing hosts
				for hostName, vars := range hostVars {
					if host, exists := mergedInventory.Hosts[hostName]; exists {
						// Host already exists, merge variables (host_vars take precedence)
						if host.Vars == nil {
							host.Vars = make(map[string]interface{})
						}
						for k, v := range vars {
							host.Vars[k] = v
						}
						common.LogDebug("Applied host_vars to existing host", map[string]interface{}{
							"host":       hostName,
							"vars_count": len(vars),
						})
					} else {
						// Create new host with variables
						newHost := &Host{
							Name: hostName,
							Host: hostName, // Default host connection to hostname
							Vars: vars,
						}
						newHost.Prepare()
						mergedInventory.Hosts[hostName] = newHost
						common.LogDebug("Created new host from host_vars", map[string]interface{}{
							"host":       hostName,
							"vars_count": len(vars),
						})
					}
				}
			}
		}
	}

	common.LogDebug("Merged inventory files with group_vars and host_vars", map[string]interface{}{
		"total_files":  len(filesToLoad),
		"total_hosts":  len(mergedInventory.Hosts),
		"total_groups": len(mergedInventory.Groups),
	})

	return mergedInventory, nil
}

func (i Inventory) GetContextForHost(host *Host, cfg *config.Config) (*HostContext, error) {
	ctx, err := InitializeHostContext(host, cfg)
	if err != nil {
		return nil, err
	}

	for k, v := range i.Vars {
		ctx.Facts.Store(k, v)
	}

	for _, group := range i.Groups {
		if group.Hosts != nil {
			if _, hostInGroup := group.Hosts[host.Name]; hostInGroup {
				if group.Vars != nil {
					for k, v := range group.Vars {
						if _, exists := host.Vars[k]; !exists {
							ctx.Facts.Store(k, v)
						}
					}
				}
			}
		}
	}

	if host.Vars != nil {
		for k, v := range host.Vars {
			ctx.Facts.Store(k, v)
		}
	}

	return ctx, nil
}

func GetContextForRun(inventory *Inventory, graph *Graph, cfg *config.Config) (map[string]*HostContext, error) {
	var err error
	contexts := make(map[string]*HostContext)
	for _, host := range inventory.Hosts {
		common.DebugOutput("Getting context for host %q", host.Name)
		contexts[host.Name], err = inventory.GetContextForHost(host, cfg)
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

// GetInitialFactsForHost gathers and layers facts for a specific host from the inventory.
// It applies global inventory vars, then group vars, then host-specific vars.
func (i Inventory) GetInitialFactsForHost(host *Host) map[string]interface{} {
	facts := make(map[string]interface{})

	// 1. Apply global inventory vars
	for k, v := range i.Vars {
		facts[k] = v
	}

	// 2. Apply group vars
	// Need to consider group hierarchy if that's a feature, but for now, iterate all groups.
	// If a host is in multiple groups, the behavior for conflicting vars might depend on group order or a defined precedence.
	// For simplicity here, we assume simple group membership. Last group var applied for a host wins for group vars.
	// Ansible has more complex group var precedence (e.g., parent groups, child groups).
	// This implementation will iterate groups as found in i.Groups map (order not guaranteed).
	// A more robust solution might sort group names or use the host.Groups field to determine relevant groups.

	// Iterate over the host's declared groups first, if available and defined with precedence
	// This part is a bit tricky without knowing exact group precedence rules Spage aims for.
	// For now, we'll stick to iterating all defined groups and checking membership.
	for groupName, group := range i.Groups { // groupName is from inventory.Groups map key
		// Check if the current host is part of this group definition
		// The inventory loading logic already flattens hosts, so direct check here is sufficient for vars.
		// A host `h` from `i.Hosts` might have `h.Groups` map indicating its memberships.
		if _, isMember := group.Hosts[host.Name]; isMember { // Check if host is explicitly listed in group's hosts
			// Alternative: check if host.Groups map contains groupName
			for k, v := range group.Vars {
				facts[k] = v // Group vars override global vars
			}
		} else {
			// Check if the host is associated with this group via its own host.Groups field
			// This handles cases where groups are assigned to hosts, rather than hosts listed under groups.
			if host.Groups != nil {
				if _, assignedToGroup := host.Groups[groupName]; assignedToGroup {
					for k, v := range group.Vars {
						facts[k] = v
					}
				}
			}
		}
	}

	// 3. Apply host-specific vars (these have the highest precedence)
	if host.Vars != nil {
		for k, v := range host.Vars {
			facts[k] = v // Host vars override group and global vars
		}
	}

	common.LogDebug("Compiled initial facts for host", map[string]interface{}{
		"host":        host.Name,
		"facts_count": len(facts),
	})
	return facts
}

func (i Inventory) GetHostByName(name string) (*Host, error) {
	host, ok := i.Hosts[name]
	if !ok {
		return nil, fmt.Errorf("host '%s' not found in inventory", name)
	}
	return host, nil
}
