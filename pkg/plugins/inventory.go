package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/types"
)

// InventoryPlugin defines the interface that all inventory plugins must implement
type InventoryPlugin interface {
	// Name returns the name of the plugin
	Name() string

	// Execute runs the plugin with the given configuration and returns inventory data
	Execute(ctx context.Context, config map[string]interface{}) (*PluginInventoryResult, error)
}

// PluginInventoryResult represents the result from an inventory plugin
type PluginInventoryResult struct {
	Hosts  map[string]*PluginHost  `json:"_meta"`
	Groups map[string]*PluginGroup `json:",inline"`
}

// PluginHost represents a host from a plugin
type PluginHost struct {
	Name string                 `json:"-"`
	Vars map[string]interface{} `json:"hostvars,omitempty"`
}

// PluginGroup represents a group from a plugin
type PluginGroup struct {
	Hosts    []string               `json:"hosts,omitempty"`
	Children []string               `json:"children,omitempty"`
	Vars     map[string]interface{} `json:"vars,omitempty"`
}

// PluginManager manages inventory plugin discovery and execution
type PluginManager struct {
	pluginDirs []string
}

// NewPluginManager creates a new plugin manager
func NewPluginManager() *PluginManager {
	return &PluginManager{
		pluginDirs: []string{
			"/usr/local/lib/spage/plugins",
			"./plugins",
			"~/.spage/plugins",
		},
	}
}

// LoadPlugin attempts to load an inventory plugin by name
func (pm *PluginManager) LoadPlugin(ctx context.Context, pluginName string, config map[string]interface{}) (*PluginInventoryResult, error) {
	common.LogDebug("Loading inventory plugin", map[string]interface{}{
		"plugin": pluginName,
		"config": config,
	})

	// Try Go plugin first
	if result, err := pm.loadGoPlugin(ctx, pluginName, config); err == nil {
		return result, nil
	} else {
		common.LogDebug("Go plugin failed, trying Python fallback", map[string]interface{}{
			"plugin": pluginName,
			"error":  err.Error(),
		})
	}

	// Fallback to Python/Ansible plugin
	return pm.loadPythonPlugin(ctx, pluginName, config)
}

// loadGoPlugin attempts to load a Go-based inventory plugin
func (pm *PluginManager) loadGoPlugin(ctx context.Context, pluginName string, config map[string]interface{}) (*PluginInventoryResult, error) {
	// Look for plugin files in plugin directories
	for _, dir := range pm.pluginDirs {
		pluginPath := filepath.Join(dir, fmt.Sprintf("spage-inventory-plugin-%s.so", pluginName))
		if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
			continue
		}

		common.LogDebug("Found Go plugin", map[string]interface{}{
			"plugin": pluginName,
			"path":   pluginPath,
		})

		// Load the plugin
		p, err := plugin.Open(pluginPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open plugin %s: %w", pluginPath, err)
		}

		// Look for the plugin symbol
		sym, err := p.Lookup("Plugin")
		if err != nil {
			return nil, fmt.Errorf("plugin %s does not export 'Plugin' symbol: %w", pluginName, err)
		}

		// Assert that it implements our interface
		inventoryPlugin, ok := sym.(InventoryPlugin)
		if !ok {
			return nil, fmt.Errorf("plugin %s does not implement InventoryPlugin interface", pluginName)
		}

		// Execute the plugin
		return inventoryPlugin.Execute(ctx, config)
	}

	return nil, fmt.Errorf("go plugin %s not found in any plugin directory", pluginName)
}

// loadPythonPlugin attempts to load a Python/Ansible-based inventory plugin
func (pm *PluginManager) loadPythonPlugin(ctx context.Context, pluginName string, config map[string]interface{}) (*PluginInventoryResult, error) {
	common.LogDebug("Attempting to load Python inventory plugin", map[string]interface{}{
		"plugin": pluginName,
	})

	// Check if ansible-inventory is available
	ansibleCmd := "ansible-inventory"
	if _, err := exec.LookPath(ansibleCmd); err != nil {
		return nil, fmt.Errorf("ansible-inventory command not found, cannot use Python plugins: %w", err)
	}

	// Create a temporary inventory file with the plugin configuration
	tempDir, err := os.MkdirTemp("", "spage-plugin-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	inventoryFile := filepath.Join(tempDir, "plugin_inventory.yml")
	inventoryContent := fmt.Sprintf("plugin: %s\n", pluginName)

	// Add configuration parameters
	for key, value := range config {
		inventoryContent += fmt.Sprintf("%s: %v\n", key, value)
	}

	if err := os.WriteFile(inventoryFile, []byte(inventoryContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write plugin inventory file: %w", err)
	}

	// Execute ansible-inventory
	cmd := exec.CommandContext(ctx, ansibleCmd, "-i", inventoryFile, "--list")
	output, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("ansible-inventory failed: %s", string(exitError.Stderr))
		}
		return nil, fmt.Errorf("failed to execute ansible-inventory: %w", err)
	}

	// Parse the JSON output
	result, err := pm.parseAnsibleInventoryOutput(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ansible-inventory output: %w", err)
	}

	common.LogDebug("Successfully loaded Python inventory plugin", map[string]interface{}{
		"plugin":       pluginName,
		"hosts_count":  len(result.Hosts),
		"groups_count": len(result.Groups),
	})

	return result, nil
}

// parseAnsibleInventoryOutput parses the JSON output from ansible-inventory
func (pm *PluginManager) parseAnsibleInventoryOutput(output []byte) (*PluginInventoryResult, error) {
	// Parse the JSON output from ansible-inventory --list
	// Format documented at: https://docs.ansible.com/ansible/latest/dev_guide/developing_inventory.html

	var rawInventory map[string]interface{}
	if err := json.Unmarshal(output, &rawInventory); err != nil {
		return nil, fmt.Errorf("failed to parse ansible-inventory JSON output: %w", err)
	}

	result := &PluginInventoryResult{
		Hosts:  make(map[string]*PluginHost),
		Groups: make(map[string]*PluginGroup),
	}

	// Parse _meta section containing host variables
	if metaData, hasMeta := rawInventory["_meta"]; hasMeta {
		if metaMap, ok := metaData.(map[string]interface{}); ok {
			if hostvars, hasHostvars := metaMap["hostvars"]; hasHostvars {
				if hostvarsMap, ok := hostvars.(map[string]interface{}); ok {
					for hostName, hostVarsData := range hostvarsMap {
						if hostVars, ok := hostVarsData.(map[string]interface{}); ok {
							result.Hosts[hostName] = &PluginHost{
								Name: hostName,
								Vars: hostVars,
							}
						}
					}
				}
			}
		}
	}

	// Parse groups (all top-level keys except _meta)
	for groupName, groupData := range rawInventory {
		if groupName == "_meta" {
			continue // Skip _meta, already processed above
		}

		if groupMap, ok := groupData.(map[string]interface{}); ok {
			group := &PluginGroup{
				Hosts:    []string{},
				Children: []string{},
				Vars:     make(map[string]interface{}),
			}

			// Parse hosts list
			if hostsData, hasHosts := groupMap["hosts"]; hasHosts {
				if hostsList, ok := hostsData.([]interface{}); ok {
					for _, hostItem := range hostsList {
						if hostName, ok := hostItem.(string); ok {
							group.Hosts = append(group.Hosts, hostName)
							// Create host entry if not exists
							if _, exists := result.Hosts[hostName]; !exists {
								result.Hosts[hostName] = &PluginHost{
									Name: hostName,
									Vars: make(map[string]interface{}),
								}
							}
						}
					}
				}
			}

			// Parse children groups
			if childrenData, hasChildren := groupMap["children"]; hasChildren {
				if childrenList, ok := childrenData.([]interface{}); ok {
					for _, childItem := range childrenList {
						if childName, ok := childItem.(string); ok {
							group.Children = append(group.Children, childName)
						}
					}
				}
			}

			// Parse group variables
			if varsData, hasVars := groupMap["vars"]; hasVars {
				if vars, ok := varsData.(map[string]interface{}); ok {
					group.Vars = vars
				}
			}

			result.Groups[groupName] = group
		}
	}

	common.LogDebug("Parsed ansible-inventory output", map[string]interface{}{
		"hosts_count":  len(result.Hosts),
		"groups_count": len(result.Groups),
	})

	return result, nil
}

// DiscoverPlugins discovers available inventory plugins
func (pm *PluginManager) DiscoverPlugins() ([]string, error) {
	var plugins []string

	// Discover Go plugins
	for _, dir := range pm.pluginDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := entry.Name()
			if strings.HasPrefix(name, "spage-inventory-plugin-") && strings.HasSuffix(name, ".so") {
				// Extract plugin name from filename
				pluginName := strings.TrimPrefix(name, "spage-inventory-plugin-")
				pluginName = strings.TrimSuffix(pluginName, ".so")
				plugins = append(plugins, pluginName)
			}
		}
	}

	common.LogDebug("Discovered inventory plugins", map[string]interface{}{
		"plugins": plugins,
	})

	return plugins, nil
}

// loadInventoryFromPlugin loads inventory from a plugin and converts to standard Inventory format
func (pm *PluginManager) LoadInventoryFromPlugin(ctx context.Context, pluginName string, config map[string]interface{}) (*Inventory, error) {
	pluginResult, err := pm.LoadPlugin(ctx, pluginName, config)
	if err != nil {
		return nil, err
	}

	// Convert plugin result to standard Inventory format
	inventory := &Inventory{
		Hosts:  make(map[string]*Host),
		Groups: make(map[string]*Group),
		Vars:   make(map[string]interface{}),
		Plugin: pluginName,
	}

	// Convert plugin hosts to standard hosts
	for hostName, pluginHost := range pluginResult.Hosts {
		host := &Host{
			Name: hostName,
			Host: hostName,
			Vars: pluginHost.Vars,
		}
		if host.Vars == nil {
			host.Vars = make(map[string]interface{})
		}
		host.Prepare()
		inventory.Hosts[hostName] = host
	}

	// Convert plugin groups to standard groups
	for groupName, pluginGroup := range pluginResult.Groups {
		group := &Group{
			Hosts: make(map[string]*Host),
			Vars:  pluginGroup.Vars,
		}
		if group.Vars == nil {
			group.Vars = make(map[string]interface{})
		}

		// Add hosts to group
		for _, hostName := range pluginGroup.Hosts {
			if host, exists := inventory.Hosts[hostName]; exists {
				group.Hosts[hostName] = host
			} else {
				// Create host if it doesn't exist
				host := &Host{
					Name: hostName,
					Host: hostName,
					Vars: make(map[string]interface{}),
				}
				host.Prepare()
				inventory.Hosts[hostName] = host
				group.Hosts[hostName] = host
			}
		}

		inventory.Groups[groupName] = group
	}

	return inventory, nil
}

// Use shared types from pkg/types to avoid duplication
type Inventory = types.Inventory
type Host = types.Host
type Group = types.Group
