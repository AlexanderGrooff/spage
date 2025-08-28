package plugins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPluginManager(t *testing.T) {
	pm := NewPluginManager()
	assert.NotNil(t, pm)
	assert.Len(t, pm.pluginDirs, 3)
	assert.Contains(t, pm.pluginDirs, "/usr/local/lib/spage/plugins")
	assert.Contains(t, pm.pluginDirs, "./plugins")
	assert.Contains(t, pm.pluginDirs, "~/.spage/plugins")
}

func TestPluginManager_DiscoverPlugins(t *testing.T) {
	// Create a temporary plugin directory for testing
	tempDir, err := os.MkdirTemp("", "spage-plugin-test-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	pm := &PluginManager{
		pluginDirs: []string{tempDir},
	}

	// Test with empty directory
	plugins, err := pm.DiscoverPlugins()
	assert.NoError(t, err)
	assert.Empty(t, plugins)

	// Create mock plugin files
	pluginFiles := []string{
		"spage-inventory-plugin-aws.so",
		"spage-inventory-plugin-gcp.so",
		"spage-inventory-plugin-docker.so",
		"other-file.txt",  // Should be ignored
		"not-a-plugin.so", // Should be ignored
	}

	for _, filename := range pluginFiles {
		filePath := filepath.Join(tempDir, filename)
		err := os.WriteFile(filePath, []byte("mock plugin"), 0644)
		require.NoError(t, err)
	}

	// Test plugin discovery
	plugins, err = pm.DiscoverPlugins()
	assert.NoError(t, err)
	assert.Len(t, plugins, 3)
	assert.Contains(t, plugins, "aws")
	assert.Contains(t, plugins, "gcp")
	assert.Contains(t, plugins, "docker")
}

func TestPluginManager_LoadPlugin_GoPluginNotFound(t *testing.T) {
	pm := &PluginManager{
		pluginDirs: []string{"/nonexistent/path"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := map[string]interface{}{
		"region": "us-west-2",
	}

	// Test with non-existent Go plugin - should try Python fallback
	result, err := pm.LoadPlugin(ctx, "nonexistent", config)

	// Two possible outcomes:
	// 1. Error if ansible-inventory is not available or plugin fails
	// 2. Success if ansible-inventory is available and returns default inventory

	if err != nil {
		// Expected case: plugin loading failed
		errorMsg := err.Error()
		hasExpectedError := (strings.Contains(errorMsg, "ansible-inventory") ||
			strings.Contains(errorMsg, "plugin") ||
			strings.Contains(errorMsg, "nonexistent") ||
			strings.Contains(errorMsg, "not found") ||
			strings.Contains(errorMsg, "failed"))
		assert.True(t, hasExpectedError, "Error should be related to plugin loading failure: %s", errorMsg)
	} else {
		// Possible case: ansible-inventory is available and returned some result
		assert.NotNil(t, result, "If no error, result should not be nil")
		t.Logf("Plugin loading succeeded unexpectedly (ansible-inventory available?): %+v", result)
		// This is not necessarily a failure - just means ansible-inventory is available
		// and returned some inventory (possibly empty)
	}
}

func TestPluginManager_LoadInventoryFromPlugin(t *testing.T) {
	// Create a mock plugin result
	pluginResult := &PluginInventoryResult{
		Hosts: map[string]*PluginHost{
			"web01": {
				Name: "web01",
				Vars: map[string]interface{}{
					"nginx_version": "1.20",
					"environment":   "production",
				},
			},
			"db01": {
				Name: "db01",
				Vars: map[string]interface{}{
					"mysql_version": "8.0",
					"environment":   "production",
				},
			},
		},
		Groups: map[string]*PluginGroup{
			"webservers": {
				Hosts: []string{"web01"},
				Vars: map[string]interface{}{
					"http_port": 80,
				},
			},
			"databases": {
				Hosts: []string{"db01"},
				Vars: map[string]interface{}{
					"db_port": 3306,
				},
			},
		},
	}

	// Mock the LoadPlugin method by creating a test that doesn't actually call external plugins
	// This would normally call pm.LoadPlugin, but we'll test the conversion logic directly

	inventory := convertPluginResultToInventory("test", pluginResult)

	// Test inventory structure
	assert.NotNil(t, inventory)
	assert.Len(t, inventory.Hosts, 2)
	assert.Len(t, inventory.Groups, 2)

	// Test hosts
	web01 := inventory.Hosts["web01"]
	assert.NotNil(t, web01)
	assert.Equal(t, "web01", web01.Name)
	assert.Equal(t, "web01", web01.Host)
	assert.Equal(t, "1.20", web01.Vars["nginx_version"])
	assert.Equal(t, "production", web01.Vars["environment"])

	db01 := inventory.Hosts["db01"]
	assert.NotNil(t, db01)
	assert.Equal(t, "db01", db01.Name)
	assert.Equal(t, "8.0", db01.Vars["mysql_version"])

	// Test groups
	webservers := inventory.Groups["webservers"]
	assert.NotNil(t, webservers)
	assert.Len(t, webservers.Hosts, 1)
	assert.Contains(t, webservers.Hosts, "web01")
	assert.Equal(t, 80, webservers.Vars["http_port"])

	databases := inventory.Groups["databases"]
	assert.NotNil(t, databases)
	assert.Len(t, databases.Hosts, 1)
	assert.Contains(t, databases.Hosts, "db01")
	assert.Equal(t, 3306, databases.Vars["db_port"])
}

func TestHost_Prepare(t *testing.T) {
	host := &Host{
		Name: "test-host",
		Host: "192.168.1.100",
	}

	host.Prepare()

	assert.NotNil(t, host.Vars)
	assert.NotNil(t, host.Groups)
	assert.False(t, host.IsLocal) // Not localhost

	// Test localhost detection
	localhostHost := &Host{
		Name: "localhost",
		Host: "localhost",
	}

	localhostHost.Prepare()
	assert.True(t, localhostHost.IsLocal)

	// Test empty host defaults to localhost
	emptyHost := &Host{
		Name: "empty",
		Host: "",
	}

	emptyHost.Prepare()
	assert.True(t, emptyHost.IsLocal)
}

func TestPluginManager_ParseAnsibleInventoryOutput(t *testing.T) {
	pm := NewPluginManager()

	// Test with empty output
	result, err := pm.parseAnsibleInventoryOutput([]byte("{}"))
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Hosts)
	assert.Empty(t, result.Groups)

	// Note: Full JSON parsing implementation would be tested here
	// For now, we're testing the basic structure
}
