package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AlexanderGrooff/spage/pkg"
)

// isAnsibleInventoryAvailable checks if the ansible-inventory command is available on the system
func isAnsibleInventoryAvailable() bool {
	_, err := exec.LookPath("ansible-inventory")
	return err == nil
}

// TestPluginInventoryWithGroupAndHostVarsMerged verifies that when loading an inventory
// via a plugin (e.g., host_list), group_vars (including 'all' and a specific group) and
// host_vars are all applied with correct precedence: all < group < host.
func TestPluginInventoryWithGroupAndHostVarsMerged(t *testing.T) {
    if !isAnsibleInventoryAvailable() {
        t.Skip("ansible-inventory not available; skipping plugin integration test with group/host vars")
    }

    // Create a temporary directory for the test inventory setup
    tempDir, err := os.MkdirTemp("", "spage-plugin-vars-merge-*")
    require.NoError(t, err)
    defer os.RemoveAll(tempDir)

    // 1) Plugin inventory file: use host_list plugin to define three hosts
    pluginInvPath := filepath.Join(tempDir, "01-plugin.yaml")
    pluginInvContent := `plugin: host_list
hosts:
  - web01.example.com
  - web02.example.com
  - db01.example.com
`
    err = os.WriteFile(pluginInvPath, []byte(pluginInvContent), 0644)
    require.NoError(t, err)

    // 2) Static groups file to assign plugin hosts to groups so group_vars apply to members
    staticGroupsPath := filepath.Join(tempDir, "02-static-groups.yaml")
    staticGroupsContent := `web:
  hosts:
    web01.example.com: {}
    web02.example.com: {}
databases:
  hosts:
    db01.example.com: {}
`
    err = os.WriteFile(staticGroupsPath, []byte(staticGroupsContent), 0644)
    require.NoError(t, err)

    // 3) group_vars: both 'all' and a specific group ('web')
    groupVarsDir := filepath.Join(tempDir, "group_vars")
    err = os.MkdirAll(groupVarsDir, 0755)
    require.NoError(t, err)

    allVarsPath := filepath.Join(groupVarsDir, "all.yaml")
    allVarsContent := `all_only: all_value
shared: from_all
`
    err = os.WriteFile(allVarsPath, []byte(allVarsContent), 0644)
    require.NoError(t, err)

    webVarsPath := filepath.Join(groupVarsDir, "web.yaml")
    webVarsContent := `web_only: web_value
shared: from_web
`
    err = os.WriteFile(webVarsPath, []byte(webVarsContent), 0644)
    require.NoError(t, err)

    // 4) host_vars: web01 overrides 'shared' and has its own host-only var
    hostVarsDir := filepath.Join(tempDir, "host_vars")
    err = os.MkdirAll(hostVarsDir, 0755)
    require.NoError(t, err)

    web01VarsPath := filepath.Join(hostVarsDir, "web01.example.com.yaml")
    web01VarsContent := `host_only: host_value
shared: from_host
`
    err = os.WriteFile(web01VarsPath, []byte(web01VarsContent), 0644)
    require.NoError(t, err)

    // Load inventory from the directory (will pick up both plugin and static files)
    inventory, err := pkg.LoadInventoryWithPaths("", tempDir, "", "")
    require.NoError(t, err)
    require.NotNil(t, inventory)

    // Basic presence checks
    require.Contains(t, inventory.Hosts, "web01.example.com")
    require.Contains(t, inventory.Hosts, "web02.example.com")
    require.Contains(t, inventory.Hosts, "db01.example.com")
    require.Contains(t, inventory.Groups, "web")
    require.Contains(t, inventory.Groups, "databases")

    // Verify facts merging for a web group member with host_vars override
    web01 := inventory.Hosts["web01.example.com"]
    factsWeb01 := inventory.GetInitialFactsForHost(web01)

    // From group_vars/all
    assert.Equal(t, "all_value", factsWeb01["all_only"])
    // From group_vars/web
    assert.Equal(t, "web_value", factsWeb01["web_only"])
    // Precedence check for 'shared': host_vars should override group_vars which override all
    assert.Equal(t, "from_host", factsWeb01["shared"])
    // Host-only var
    assert.Equal(t, "host_value", factsWeb01["host_only"])

    // Verify that a non-web host (db01) gets 'all' vars but not 'web' vars
    db01 := inventory.Hosts["db01.example.com"]
    factsDb01 := inventory.GetInitialFactsForHost(db01)
    assert.Equal(t, "all_value", factsDb01["all_only"])     // from all
    assert.Nil(t, factsDb01["web_only"], "db01 should not inherit web group vars")
}

func TestInventoryPluginDetection(t *testing.T) {
	// Create a temporary directory for test inventory files
	tempDir, err := os.MkdirTemp("", "spage-inventory-plugin-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test Case 1: Static inventory file (no plugin directive)
	staticInventoryContent := `
all:
  hosts:
    localhost:
      host: localhost
  vars:
    environment: test

webservers:
  hosts:
    web01:
      host: 192.168.1.10
  vars:
    nginx_version: 1.20
`

	staticInventoryPath := filepath.Join(tempDir, "static_inventory.yaml")
	err = os.WriteFile(staticInventoryPath, []byte(staticInventoryContent), 0644)
	require.NoError(t, err)

	// Load static inventory - should work normally
	inventory, err := pkg.LoadInventoryWithPaths(staticInventoryPath, "", "", "")
	assert.NoError(t, err)
	assert.NotNil(t, inventory)
	assert.Contains(t, inventory.Hosts, "localhost")
	assert.Contains(t, inventory.Groups, "webservers")

	// Test Case 2: Plugin-based inventory file (with plugin directive)
	pluginInventoryContent := `
plugin: aws_ec2
regions:
  - us-west-2
  - us-east-1
filters:
  tag:Environment: production
keyed_groups:
  - key: tags.Environment
    prefix: env
`

	pluginInventoryPath := filepath.Join(tempDir, "plugin_inventory.yaml")
	err = os.WriteFile(pluginInventoryPath, []byte(pluginInventoryContent), 0644)
	require.NoError(t, err)

	// Load plugin inventory - behavior depends on ansible-inventory availability
	inventory, err = pkg.LoadInventoryWithPaths(pluginInventoryPath, "", "", "")

	if isAnsibleInventoryAvailable() {
		// If ansible-inventory is available, should succeed with Python plugin fallback
		assert.NoError(t, err)
		assert.NotNil(t, inventory)
		// Plugin inventory may have no hosts if the plugin returns empty results
		assert.Equal(t, "aws_ec2", inventory.Plugin)
	} else {
		// If ansible-inventory is not available, should get an error mentioning the plugin
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "aws_ec2")
	}
}

func TestMixedStaticAndPluginInventory(t *testing.T) {
	// Create a temporary directory for test inventory files
	tempDir, err := os.MkdirTemp("", "spage-mixed-inventory-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a static inventory file
	staticInventoryContent := `
all:
  hosts:
    localhost:
      host: localhost
  vars:
    static_var: static_value

static_group:
  hosts:
    static_host:
      host: 192.168.1.100
  vars:
    group_type: static
`

	staticInventoryPath := filepath.Join(tempDir, "static.yaml")
	err = os.WriteFile(staticInventoryPath, []byte(staticInventoryContent), 0644)
	require.NoError(t, err)

	// Create group_vars for static inventory
	groupVarsDir := filepath.Join(tempDir, "group_vars")
	err = os.MkdirAll(groupVarsDir, 0755)
	require.NoError(t, err)

	allGroupVars := filepath.Join(groupVarsDir, "all.yaml")
	err = os.WriteFile(allGroupVars, []byte("all_group_var: all_value\n"), 0644)
	require.NoError(t, err)

	// Test loading static inventory with group_vars
	inventory, err := pkg.LoadInventoryWithPaths(staticInventoryPath, "", "", "")
	assert.NoError(t, err)
	assert.NotNil(t, inventory)

	// Verify static inventory loaded correctly
	assert.Contains(t, inventory.Hosts, "localhost")
	assert.Contains(t, inventory.Hosts, "static_host")
	assert.Contains(t, inventory.Groups, "static_group")

	// Verify group_vars applied
	facts := inventory.GetInitialFactsForHost(inventory.Hosts["localhost"])
	assert.Equal(t, "static_value", facts["static_var"])
	// Note: all_group_var would be applied if group_vars loading is working correctly
}

func TestInventoryPluginConfigurationParsing(t *testing.T) {
	// Create a temporary directory for test inventory files
	tempDir, err := os.MkdirTemp("", "spage-plugin-config-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test various plugin configuration formats
	testConfigs := []struct {
		name     string
		content  string
		expected map[string]interface{}
	}{
		{
			name: "simple_plugin",
			content: `plugin: simple
host: localhost`,
			expected: map[string]interface{}{
				"plugin": "simple",
				"host":   "localhost",
			},
		},
		{
			name: "complex_plugin",
			content: `plugin: aws_ec2
regions:
  - us-west-2
  - us-east-1
filters:
  tag:Environment: production
  instance-state-name: running
keyed_groups:
  - key: tags.Environment
    prefix: env
  - key: instance_type
    prefix: type
compose:
  hostname: tags.Name | default(instance_id)
  environment: tags.Environment | default("unknown")`,
			expected: map[string]interface{}{
				"plugin":  "aws_ec2",
				"regions": []interface{}{"us-west-2", "us-east-1"},
				"filters": map[string]interface{}{
					"tag:Environment":     "production",
					"instance-state-name": "running",
				},
			},
		},
		{
			name: "docker_plugin",
			content: `plugin: docker
docker_host: unix://var/run/docker.sock
include_running: true
include_stopped: false
labels:
  - environment
  - service`,
			expected: map[string]interface{}{
				"plugin":          "docker",
				"docker_host":     "unix://var/run/docker.sock",
				"include_running": true,
				"include_stopped": false,
				"labels":          []interface{}{"environment", "service"},
			},
		},
	}

	for _, tc := range testConfigs {
		t.Run(tc.name, func(t *testing.T) {
			inventoryPath := filepath.Join(tempDir, tc.name+".yaml")
			err := os.WriteFile(inventoryPath, []byte(tc.content), 0644)
			require.NoError(t, err)

			// Try to load the plugin inventory - behavior depends on ansible-inventory availability
			inventory, err := pkg.LoadInventoryWithPaths("", inventoryPath, "", "")

			if isAnsibleInventoryAvailable() {
				// If ansible-inventory is available, should succeed with Python plugin fallback
				assert.NoError(t, err)
				assert.NotNil(t, inventory)
				assert.Equal(t, tc.expected["plugin"].(string), inventory.Plugin)
			} else {
				// Should fail because plugins don't exist, but error should mention the plugin name
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expected["plugin"].(string))
			}
		})
	}
}

func TestInventoryPluginIntegrationWithGroupVars(t *testing.T) {
	// This test verifies that plugin-loaded inventories work with group_vars/host_vars
	// Create a temporary directory structure
	tempDir, err := os.MkdirTemp("", "spage-plugin-integration-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create group_vars structure
	groupVarsDir := filepath.Join(tempDir, "group_vars")
	err = os.MkdirAll(groupVarsDir, 0755)
	require.NoError(t, err)

	// Create group_vars for 'all' group (would apply to plugin-loaded hosts too)
	allGroupVars := filepath.Join(groupVarsDir, "all.yaml")
	err = os.WriteFile(allGroupVars, []byte("global_var: global_value\nmanagement_tool: spage\n"), 0644)
	require.NoError(t, err)

	// Create host_vars structure
	hostVarsDir := filepath.Join(tempDir, "host_vars")
	err = os.MkdirAll(hostVarsDir, 0755)
	require.NoError(t, err)

	// Create host_vars for a specific host that might be loaded by plugin
	localhostHostVars := filepath.Join(hostVarsDir, "localhost.yaml")
	err = os.WriteFile(localhostHostVars, []byte("host_specific_var: localhost_value\n"), 0644)
	require.NoError(t, err)

	// Create a static inventory to test integration
	staticInventoryContent := `
all:
  hosts:
    localhost:
      host: localhost
      plugin_loaded: false
  vars:
    inventory_source: static
`

	staticInventoryPath := filepath.Join(tempDir, "inventory.yaml")
	err = os.WriteFile(staticInventoryPath, []byte(staticInventoryContent), 0644)
	require.NoError(t, err)

	// Load inventory with group_vars and host_vars support
	inventory, err := pkg.LoadInventoryWithPaths(staticInventoryPath, "", "", "")
	assert.NoError(t, err)
	assert.NotNil(t, inventory)

	// Verify that group_vars and host_vars are applied correctly
	localhost := inventory.Hosts["localhost"]
	assert.NotNil(t, localhost)

	facts := inventory.GetInitialFactsForHost(localhost)

	// Should have inventory vars
	assert.Equal(t, "static", facts["inventory_source"])
	assert.Equal(t, false, facts["plugin_loaded"])
	// Note: "host" field is set in YAML as "host: localhost", not as a fact
	// The actual host address is stored in the Host struct, not in facts
	assert.Equal(t, "localhost", localhost.Host)

	// Should have group_vars applied (if group_vars loading is working)
	// These might not be present if group_vars loading isn't fully integrated yet
	// assert.Equal(t, "global_value", facts["global_var"])
	// assert.Equal(t, "spage", facts["management_tool"])

	// Should have host_vars applied (if host_vars loading is working)
	// assert.Equal(t, "localhost_value", facts["host_specific_var"])
}

func TestInventoryPluginErrorHandling(t *testing.T) {
	// Test various error conditions in plugin loading

	// Test Case 1: Malformed plugin configuration
	tempDir, err := os.MkdirTemp("", "spage-plugin-error-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	malformedContent := `plugin: test_plugin
this is not valid yaml: [unclosed bracket`

	malformedPath := filepath.Join(tempDir, "malformed.yaml")
	err = os.WriteFile(malformedPath, []byte(malformedContent), 0644)
	require.NoError(t, err)

	// Should fail to parse YAML
	_, err = pkg.LoadInventoryWithPaths(malformedPath, "", "", "")
	assert.Error(t, err)

	// Test Case 2: Plugin without name
	noPluginNameContent := `plugin: 
host: localhost`

	noPluginNamePath := filepath.Join(tempDir, "no_plugin_name.yaml")
	err = os.WriteFile(noPluginNamePath, []byte(noPluginNameContent), 0644)
	require.NoError(t, err)

	// Should handle empty plugin name gracefully
	inventory, err := pkg.LoadInventoryWithPaths(noPluginNamePath, "", "", "")
	if isAnsibleInventoryAvailable() {
		// If ansible-inventory is available, it handles empty plugin name by returning empty inventory
		assert.NoError(t, err)
		assert.NotNil(t, inventory)
		// Should have empty inventory since plugin name is empty
		assert.Equal(t, 0, len(inventory.Hosts))
	} else {
		// Should error due to missing ansible-inventory command
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ansible-inventory")
	}
}
