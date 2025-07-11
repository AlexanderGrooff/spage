package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestSplitInventoryPaths tests the splitInventoryPaths function
func TestSplitInventoryPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "single path",
			input:    "/opt/inventory",
			expected: []string{"/opt/inventory"},
		},
		{
			name:     "multiple paths",
			input:    "/opt/inventory:/home/user/inventory:./inventory",
			expected: []string{"/opt/inventory", "/home/user/inventory", "./inventory"},
		},
		{
			name:     "paths with spaces",
			input:    "/opt/inventory : /home/user/inventory : ./inventory",
			expected: []string{"/opt/inventory", "/home/user/inventory", "./inventory"},
		},
		{
			name:     "empty paths in string",
			input:    "/opt/inventory::/home/user/inventory:",
			expected: []string{"/opt/inventory", "/home/user/inventory"},
		},
		{
			name:     "only colons",
			input:    ":::",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitInventoryPaths(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d paths, got %d", len(tt.expected), len(result))
				return
			}
			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("Expected path %d to be %s, got %s", i, expected, result[i])
				}
			}
		})
	}
}

// TestFindInventoryFile tests the findInventoryFile function
func TestFindInventoryFile(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-inventory-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create test inventory directories and files
	inv1Dir := filepath.Join(tmpDir, "inventory1")
	inv2Dir := filepath.Join(tmpDir, "inventory2")
	inv3Dir := filepath.Join(tmpDir, "inventory3")

	// Create directory structures
	for _, dir := range []string{inv1Dir, inv2Dir, inv3Dir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create inventory directory %s: %v", dir, err)
		}
	}

	// Create inventory files
	inv1File := filepath.Join(inv1Dir, "inventory.yml")
	inv2File := filepath.Join(inv2Dir, "hosts")
	inv3File := filepath.Join(tmpDir, "direct-inventory.yaml")

	inventoryContent := `
all:
  test-host:
    host: localhost
`

	// Write inventory files
	if err := os.WriteFile(inv1File, []byte(inventoryContent), 0644); err != nil {
		t.Fatalf("Failed to write inventory file %s: %v", inv1File, err)
	}
	if err := os.WriteFile(inv2File, []byte(inventoryContent), 0644); err != nil {
		t.Fatalf("Failed to write inventory file %s: %v", inv2File, err)
	}
	if err := os.WriteFile(inv3File, []byte(inventoryContent), 0644); err != nil {
		t.Fatalf("Failed to write inventory file %s: %v", inv3File, err)
	}

	tests := []struct {
		name           string
		inventoryPaths []string
		workingDir     string
		expectedFile   string
		shouldError    bool
	}{
		{
			name:           "find inventory.yml in first directory",
			inventoryPaths: []string{inv1Dir, inv2Dir},
			workingDir:     tmpDir,
			expectedFile:   inv1File,
			shouldError:    false,
		},
		{
			name:           "find hosts file in second directory",
			inventoryPaths: []string{"/nonexistent", inv2Dir},
			workingDir:     tmpDir,
			expectedFile:   inv2File,
			shouldError:    false,
		},
		{
			name:           "find direct file path",
			inventoryPaths: []string{inv3File},
			workingDir:     tmpDir,
			expectedFile:   inv3File,
			shouldError:    false,
		},
		{
			name:           "relative path",
			inventoryPaths: []string{"inventory1", "inventory2"},
			workingDir:     tmpDir,
			expectedFile:   inv1File,
			shouldError:    false,
		},
		{
			name:           "no inventory found",
			inventoryPaths: []string{"/nonexistent1", "/nonexistent2"},
			workingDir:     tmpDir,
			expectedFile:   "",
			shouldError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := findInventoryFile(tt.inventoryPaths, tt.workingDir)

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result != tt.expectedFile {
				t.Errorf("Expected file %s, got %s", tt.expectedFile, result)
			}
		})
	}
}

// TestLoadInventoryWithPaths tests the LoadInventoryWithPaths function
func TestLoadInventoryWithPaths(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-inventory-load-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create test inventory directories and files
	invDir := filepath.Join(tmpDir, "inventories")
	if err := os.MkdirAll(invDir, 0755); err != nil {
		t.Fatalf("Failed to create inventory directory: %v", err)
	}

	// Create an inventory file
	invFile := filepath.Join(invDir, "inventory.yml")
	inventoryContent := `
all:
  test-host:
    host: localhost
    custom_var: test_value
`
	if err := os.WriteFile(invFile, []byte(inventoryContent), 0644); err != nil {
		t.Fatalf("Failed to write inventory file: %v", err)
	}

	tests := []struct {
		name              string
		path              string
		inventoryPaths    string
		workingDir        string
		expectedHosts     int
		expectedLocalhost bool
	}{
		{
			name:              "explicit path takes precedence",
			path:              invFile,
			inventoryPaths:    "/nonexistent",
			workingDir:        tmpDir,
			expectedHosts:     1,
			expectedLocalhost: false,
		},
		{
			name:              "use inventory paths when no explicit path",
			path:              "",
			inventoryPaths:    invDir,
			workingDir:        tmpDir,
			expectedHosts:     1,
			expectedLocalhost: false,
		},
		{
			name:              "fall back to localhost when no inventory found",
			path:              "",
			inventoryPaths:    "/nonexistent",
			workingDir:        tmpDir,
			expectedHosts:     1,
			expectedLocalhost: true,
		},
		{
			name:              "fall back to localhost when no path or inventory paths",
			path:              "",
			inventoryPaths:    "",
			workingDir:        tmpDir,
			expectedHosts:     1,
			expectedLocalhost: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inventory, err := LoadInventoryWithPaths(tt.path, tt.inventoryPaths, tt.workingDir)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(inventory.Hosts) != tt.expectedHosts {
				t.Errorf("Expected %d hosts, got %d", tt.expectedHosts, len(inventory.Hosts))
			}

			if tt.expectedLocalhost {
				if _, exists := inventory.Hosts["localhost"]; !exists {
					t.Error("Expected localhost host to exist")
				}
			} else {
				if _, exists := inventory.Hosts["test-host"]; !exists {
					t.Error("Expected test-host to exist")
				}
			}
		})
	}
}

// TestFindAllInventoryFiles tests the findAllInventoryFiles function
func TestFindAllInventoryFiles(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-inventory-all-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create test inventory directories and files
	inv1Dir := filepath.Join(tmpDir, "inventory1")
	inv2Dir := filepath.Join(tmpDir, "inventory2")
	inv3Dir := filepath.Join(tmpDir, "inventory3")

	// Create directory structures
	for _, dir := range []string{inv1Dir, inv2Dir, inv3Dir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create inventory directory %s: %v", dir, err)
		}
	}

	// Create inventory files with various names (Ansible loads ALL files in a directory)
	inv1File1 := filepath.Join(inv1Dir, "production.yml")
	inv1File2 := filepath.Join(inv1Dir, "staging.ini")
	inv2File1 := filepath.Join(inv2Dir, "hosts")
	inv2File2 := filepath.Join(inv2Dir, "web-servers.yaml")
	inv3File := filepath.Join(tmpDir, "direct-inventory.yaml")

	inventoryContent := `
all:
  test-host:
    host: localhost
`

	// Write inventory files
	filesToCreate := []string{inv1File1, inv1File2, inv2File1, inv2File2, inv3File}
	for _, file := range filesToCreate {
		if err := os.WriteFile(file, []byte(inventoryContent), 0644); err != nil {
			t.Fatalf("Failed to write inventory file %s: %v", file, err)
		}
	}

	tests := []struct {
		name           string
		inventoryPaths []string
		workingDir     string
		expectedFiles  []string
		shouldError    bool
	}{
		{
			name:           "find all files in multiple directories",
			inventoryPaths: []string{inv1Dir, inv2Dir},
			workingDir:     tmpDir,
			expectedFiles:  []string{inv1File1, inv1File2, inv2File1, inv2File2}, // All files, sorted alphabetically
			shouldError:    false,
		},
		{
			name:           "find files including nonexistent path",
			inventoryPaths: []string{"/nonexistent", inv1Dir},
			workingDir:     tmpDir,
			expectedFiles:  []string{inv1File1, inv1File2}, // All files from inv1Dir
			shouldError:    false,
		},
		{
			name:           "find direct file paths",
			inventoryPaths: []string{inv3File, inv1File1},
			workingDir:     tmpDir,
			expectedFiles:  []string{inv3File, inv1File1},
			shouldError:    false,
		},
		{
			name:           "relative paths",
			inventoryPaths: []string{"inventory1", "inventory2"},
			workingDir:     tmpDir,
			expectedFiles:  []string{inv1File1, inv1File2, inv2File1, inv2File2},
			shouldError:    false,
		},
		{
			name:           "no inventory found",
			inventoryPaths: []string{"/nonexistent1", "/nonexistent2"},
			workingDir:     tmpDir,
			expectedFiles:  nil,
			shouldError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := findAllInventoryFiles(tt.inventoryPaths, tt.workingDir)

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(result) != len(tt.expectedFiles) {
				t.Errorf("Expected %d files, got %d", len(tt.expectedFiles), len(result))
				t.Errorf("Expected: %v", tt.expectedFiles)
				t.Errorf("Got: %v", result)
				return
			}

			// Check that all expected files are present in the correct order
			for i, expectedFile := range tt.expectedFiles {
				if result[i] != expectedFile {
					t.Errorf("Expected file %d to be %s, got %s", i, expectedFile, result[i])
				}
			}
		})
	}
}

// TestLoadMultipleInventoryFiles tests loading and merging multiple inventory files
func TestLoadMultipleInventoryFiles(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-inventory-multiple-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create test inventory directories
	inv1Dir := filepath.Join(tmpDir, "inventory1")
	inv2Dir := filepath.Join(tmpDir, "inventory2")

	for _, dir := range []string{inv1Dir, inv2Dir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create inventory directory %s: %v", dir, err)
		}
	}

	// Create first inventory file
	inv1File := filepath.Join(inv1Dir, "inventory.yml")
	inv1Content := `
all:
  host1:
    host: server1.example.com
    env: production
  host2:
    host: server2.example.com
    env: production
vars:
  global_var: value1
groups:
  webservers:
    hosts:
      host1: {}
    vars:
      web_port: 80
`

	// Create second inventory file
	inv2File := filepath.Join(inv2Dir, "inventory.yml")
	inv2Content := `
all:
  host2:
    host: server2-updated.example.com
    env: staging
  host3:
    host: server3.example.com
    env: development
vars:
  global_var: value2
  another_var: another_value
groups:
  webservers:
    hosts:
      host3: {}
    vars:
      web_port: 8080
  databases:
    hosts:
      host2: {}
    vars:
      db_port: 5432
`

	if err := os.WriteFile(inv1File, []byte(inv1Content), 0644); err != nil {
		t.Fatalf("Failed to write inventory file %s: %v", inv1File, err)
	}
	if err := os.WriteFile(inv2File, []byte(inv2Content), 0644); err != nil {
		t.Fatalf("Failed to write inventory file %s: %v", inv2File, err)
	}

	// Test loading multiple inventory files
	inventoryPaths := fmt.Sprintf("%s:%s", inv1Dir, inv2Dir)
	inventory, err := LoadInventoryWithPaths("", inventoryPaths, tmpDir)
	if err != nil {
		t.Fatalf("Unexpected error loading inventories: %v", err)
	}

	// Verify hosts
	expectedHosts := []string{"host1", "host2", "host3"}
	if len(inventory.Hosts) != len(expectedHosts) {
		t.Errorf("Expected %d hosts, got %d", len(expectedHosts), len(inventory.Hosts))
	}

	for _, hostName := range expectedHosts {
		if _, exists := inventory.Hosts[hostName]; !exists {
			t.Errorf("Expected host %s to exist", hostName)
		}
	}

	// Verify host2 was overridden by second inventory
	host2 := inventory.Hosts["host2"]
	if host2.Host != "server2-updated.example.com" {
		t.Errorf("Expected host2.Host to be 'server2-updated.example.com', got '%s'", host2.Host)
	}
	if host2.Vars["env"] != "staging" {
		t.Errorf("Expected host2.env to be 'staging', got '%v'", host2.Vars["env"])
	}

	// Verify host3 was overridden by second inventory
	host3 := inventory.Hosts["host3"]
	if host3.Host != "server3.example.com" {
		t.Errorf("Expected host3.Host to be 'server3.example.com', got '%s'", host3.Host)
	}
	if host3.Vars["env"] != "development" {
		t.Errorf("Expected host3.env to be 'development', got '%v'", host3.Vars["env"])
	}
	if host3.Vars["web_port"] != 8080 {
		t.Errorf("Expected host3.web_port to be '8080', got '%v'", host3.Vars["web_port"])
	}

	// Verify global vars (second inventory should override)
	if inventory.Vars["global_var"] != "value2" {
		t.Errorf("Expected global_var to be 'value2', got '%v'", inventory.Vars["global_var"])
	}
	if inventory.Vars["another_var"] != "another_value" {
		t.Errorf("Expected another_var to be 'another_value', got '%v'", inventory.Vars["another_var"])
	}

	// Verify groups were merged
	expectedGroups := []string{"webservers", "databases"}
	if len(inventory.Groups) != len(expectedGroups) {
		t.Errorf("Expected %d groups, got %d", len(expectedGroups), len(inventory.Groups))
	}

	// Verify webservers group has hosts from both inventories
	webservers := inventory.Groups["webservers"]
	if len(webservers.Hosts) != 2 {
		t.Errorf("Expected webservers group to have 2 hosts, got %d", len(webservers.Hosts))
	}
	if _, exists := webservers.Hosts["host1"]; !exists {
		t.Error("Expected host1 to be in webservers group")
	}
	if _, exists := webservers.Hosts["host3"]; !exists {
		t.Error("Expected host3 to be in webservers group")
	}

	// Verify webservers group vars were overridden
	if webservers.Vars["web_port"] != 8080 {
		t.Errorf("Expected webservers web_port to be 8080, got %v", webservers.Vars["web_port"])
	}

	// Verify databases group exists
	if _, exists := inventory.Groups["databases"]; !exists {
		t.Error("Expected databases group to exist")
	}
}

// TestLoadAllFilesInDirectory tests that we load ALL files in a directory like Ansible does
func TestLoadAllFilesInDirectory(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-ansible-behavior-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create an inventory directory with files that have various names and extensions
	invDir := filepath.Join(tmpDir, "inventory")
	if err := os.MkdirAll(invDir, 0755); err != nil {
		t.Fatalf("Failed to create inventory directory: %v", err)
	}

	// Create various inventory files with different names but YAML format
	// This mimics real-world Ansible usage where you might have files with different names
	files := map[string]string{
		"01-production.yml": `
all:
  prod-web:
    host: prod-web.example.com
    env: production`,
		"02-staging.yaml": `
all:
  staging-web:
    host: staging-web.example.com
    env: staging`,
		"databases": `
all:
  db-server:
    host: db.example.com
    role: database`,
		"web-servers.yaml": `
all:
  web1:
    host: web1.example.com`,
		"zz-loadbalancers": `
all:
  lb1:
    host: lb1.example.com`,
	}

	for fileName, content := range files {
		filePath := filepath.Join(invDir, fileName)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", fileName, err)
		}
	}

	// Load inventory from the directory
	inventory, err := LoadInventoryWithPaths("", invDir, tmpDir)
	if err != nil {
		t.Fatalf("Failed to load inventory: %v", err)
	}

	// Check that hosts from ALL files were loaded
	expectedHosts := []string{"prod-web", "staging-web", "db-server", "web1", "lb1"}
	if len(inventory.Hosts) != len(expectedHosts) {
		t.Errorf("Expected %d hosts, got %d", len(expectedHosts), len(inventory.Hosts))
		t.Errorf("Hosts found: %v", func() []string {
			var hosts []string
			for name := range inventory.Hosts {
				hosts = append(hosts, name)
			}
			return hosts
		}())
	}

	for _, hostName := range expectedHosts {
		if _, exists := inventory.Hosts[hostName]; !exists {
			t.Errorf("Expected host %s to exist (from various file types)", hostName)
		}
	}

	// Verify that files were processed in alphabetical order
	// The files should be loaded in this order: 01-production.yml, 02-staging.yaml, databases, web-servers.yaml, zz-loadbalancers

	t.Logf("Successfully loaded %d hosts from %d inventory files with various names",
		len(inventory.Hosts), len(files))
}
