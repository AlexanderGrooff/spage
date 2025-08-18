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
			inventory, err := LoadInventoryWithPaths(tt.path, tt.inventoryPaths, tt.workingDir, "")
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
	inventory, err := LoadInventoryWithPaths("", inventoryPaths, tmpDir, "")
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
	inventory, err := LoadInventoryWithPaths("", invDir, tmpDir, "")
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

// TestLoadGroupVars tests the loadGroupVars function
func TestLoadGroupVars(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-group-vars-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create group_vars directory structure
	groupVarsDir := filepath.Join(tmpDir, "group_vars")
	if err := os.MkdirAll(groupVarsDir, 0755); err != nil {
		t.Fatalf("Failed to create group_vars directory: %v", err)
	}

	// Test 1: group_vars/web.yml file structure
	webGroupFile := filepath.Join(groupVarsDir, "web.yml")
	webGroupContent := `
nginx_port: 80
ssl_enabled: true
domain: example.com
`
	if err := os.WriteFile(webGroupFile, []byte(webGroupContent), 0644); err != nil {
		t.Fatalf("Failed to write web group vars file: %v", err)
	}

	// Test 2: group_vars/database/ directory structure
	dbGroupDir := filepath.Join(groupVarsDir, "database")
	if err := os.MkdirAll(dbGroupDir, 0755); err != nil {
		t.Fatalf("Failed to create database group directory: %v", err)
	}

	dbMainFile := filepath.Join(dbGroupDir, "main.yml")
	dbMainContent := `
db_port: 5432
db_name: myapp
`
	if err := os.WriteFile(dbMainFile, []byte(dbMainContent), 0644); err != nil {
		t.Fatalf("Failed to write database main vars file: %v", err)
	}

	dbSecretFile := filepath.Join(dbGroupDir, "secret.yml")
	dbSecretContent := `
db_password: secret123
api_key: abc123
`
	if err := os.WriteFile(dbSecretFile, []byte(dbSecretContent), 0644); err != nil {
		t.Fatalf("Failed to write database secret vars file: %v", err)
	}

	// Test 3: Invalid YAML file (should be ignored)
	invalidFile := filepath.Join(groupVarsDir, "invalid.yml")
	invalidContent := `
invalid: yaml: content
  - bad
`
	if err := os.WriteFile(invalidFile, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("Failed to write invalid vars file: %v", err)
	}

	// Test 4: Non-YAML file (should be ignored)
	nonYamlFile := filepath.Join(groupVarsDir, "readme.txt")
	if err := os.WriteFile(nonYamlFile, []byte("This is not a YAML file"), 0644); err != nil {
		t.Fatalf("Failed to write non-YAML file: %v", err)
	}

	// Load group variables
	groupVars, err := loadGroupVars(tmpDir)
	if err != nil {
		t.Fatalf("Failed to load group vars: %v", err)
	}

	// Verify web group vars (from file)
	webVars, exists := groupVars["web"]
	if !exists {
		t.Error("Expected web group vars to be loaded")
	} else {
		if webVars["nginx_port"] != 80 {
			t.Errorf("Expected nginx_port to be 80, got %v", webVars["nginx_port"])
		}
		if webVars["ssl_enabled"] != true {
			t.Errorf("Expected ssl_enabled to be true, got %v", webVars["ssl_enabled"])
		}
		if webVars["domain"] != "example.com" {
			t.Errorf("Expected domain to be 'example.com', got %v", webVars["domain"])
		}
	}

	// Verify database group vars (from directory)
	dbVars, exists := groupVars["database"]
	if !exists {
		t.Error("Expected database group vars to be loaded")
	} else {
		// Should have variables from both files
		if dbVars["db_port"] != 5432 {
			t.Errorf("Expected db_port to be 5432, got %v", dbVars["db_port"])
		}
		if dbVars["db_name"] != "myapp" {
			t.Errorf("Expected db_name to be 'myapp', got %v", dbVars["db_name"])
		}
		if dbVars["db_password"] != "secret123" {
			t.Errorf("Expected db_password to be 'secret123', got %v", dbVars["db_password"])
		}
		if dbVars["api_key"] != "abc123" {
			t.Errorf("Expected api_key to be 'abc123', got %v", dbVars["api_key"])
		}
	}

	// Verify invalid group vars are not loaded
	if _, exists := groupVars["invalid"]; exists {
		t.Error("Invalid YAML file should not be loaded")
	}

	// Test empty directory
	emptyDir, err := os.MkdirTemp("", "spage-empty-group-vars-test")
	if err != nil {
		t.Fatalf("Failed to create empty temp dir: %v", err)
	}
	defer os.RemoveAll(emptyDir)

	emptyGroupVars, err := loadGroupVars(emptyDir)
	if err != nil {
		t.Fatalf("Failed to load from empty directory: %v", err)
	}
	if len(emptyGroupVars) != 0 {
		t.Errorf("Expected empty group vars, got %d entries", len(emptyGroupVars))
	}
}

// TestLoadHostVars tests the loadHostVars function
func TestLoadHostVars(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-host-vars-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create host_vars directory structure
	hostVarsDir := filepath.Join(tmpDir, "host_vars")
	if err := os.MkdirAll(hostVarsDir, 0755); err != nil {
		t.Fatalf("Failed to create host_vars directory: %v", err)
	}

	// Test 1: host_vars/web01.yml file structure
	web01File := filepath.Join(hostVarsDir, "web01.yml")
	web01Content := `
server_id: 1
role: primary
memory_gb: 8
`
	if err := os.WriteFile(web01File, []byte(web01Content), 0644); err != nil {
		t.Fatalf("Failed to write web01 host vars file: %v", err)
	}

	// Test 2: host_vars/db01/ directory structure
	db01Dir := filepath.Join(hostVarsDir, "db01")
	if err := os.MkdirAll(db01Dir, 0755); err != nil {
		t.Fatalf("Failed to create db01 host directory: %v", err)
	}

	db01MainFile := filepath.Join(db01Dir, "main.yml")
	db01MainContent := `
server_id: 10
role: master
cpu_cores: 16
`
	if err := os.WriteFile(db01MainFile, []byte(db01MainContent), 0644); err != nil {
		t.Fatalf("Failed to write db01 main vars file: %v", err)
	}

	db01NetworkFile := filepath.Join(db01Dir, "network.yml")
	db01NetworkContent := `
private_ip: 10.0.1.10
public_ip: 203.0.113.10
`
	if err := os.WriteFile(db01NetworkFile, []byte(db01NetworkContent), 0644); err != nil {
		t.Fatalf("Failed to write db01 network vars file: %v", err)
	}

	// Load host variables
	hostVars, err := loadHostVars(tmpDir)
	if err != nil {
		t.Fatalf("Failed to load host vars: %v", err)
	}

	// Verify web01 host vars (from file)
	web01Vars, exists := hostVars["web01"]
	if !exists {
		t.Error("Expected web01 host vars to be loaded")
	} else {
		if web01Vars["server_id"] != 1 {
			t.Errorf("Expected server_id to be 1, got %v", web01Vars["server_id"])
		}
		if web01Vars["role"] != "primary" {
			t.Errorf("Expected role to be 'primary', got %v", web01Vars["role"])
		}
		if web01Vars["memory_gb"] != 8 {
			t.Errorf("Expected memory_gb to be 8, got %v", web01Vars["memory_gb"])
		}
	}

	// Verify db01 host vars (from directory)
	db01Vars, exists := hostVars["db01"]
	if !exists {
		t.Error("Expected db01 host vars to be loaded")
	} else {
		// Should have variables from both files
		if db01Vars["server_id"] != 10 {
			t.Errorf("Expected server_id to be 10, got %v", db01Vars["server_id"])
		}
		if db01Vars["role"] != "master" {
			t.Errorf("Expected role to be 'master', got %v", db01Vars["role"])
		}
		if db01Vars["cpu_cores"] != 16 {
			t.Errorf("Expected cpu_cores to be 16, got %v", db01Vars["cpu_cores"])
		}
		if db01Vars["private_ip"] != "10.0.1.10" {
			t.Errorf("Expected private_ip to be '10.0.1.10', got %v", db01Vars["private_ip"])
		}
		if db01Vars["public_ip"] != "203.0.113.10" {
			t.Errorf("Expected public_ip to be '203.0.113.10', got %v", db01Vars["public_ip"])
		}
	}
}

// TestLoadInventoryWithGroupAndHostVars tests the integration of group_vars and host_vars
func TestLoadInventoryWithGroupAndHostVars(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-inventory-vars-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create main inventory file
	inventoryFile := filepath.Join(tmpDir, "inventory.yml")
	inventoryContent := `
all:
  web01:
    host: 192.168.1.10
  web02:
    host: 192.168.1.11
  db01:
    host: 192.168.1.20
groups:
  webservers:
    hosts:
      web01: {}
      web02: {}
    vars:
      http_port: 80
  databases:
    hosts:
      db01: {}
    vars:
      db_port: 5432
vars:
  environment: production
`
	if err := os.WriteFile(inventoryFile, []byte(inventoryContent), 0644); err != nil {
		t.Fatalf("Failed to write inventory file: %v", err)
	}

	// Create group_vars directory and files
	groupVarsDir := filepath.Join(tmpDir, "group_vars")
	if err := os.MkdirAll(groupVarsDir, 0755); err != nil {
		t.Fatalf("Failed to create group_vars directory: %v", err)
	}

	webserversGroupFile := filepath.Join(groupVarsDir, "webservers.yml")
	webserversGroupContent := `
nginx_version: "1.20"
ssl_cert_path: "/etc/ssl/certs/server.crt"
max_connections: 1000
`
	if err := os.WriteFile(webserversGroupFile, []byte(webserversGroupContent), 0644); err != nil {
		t.Fatalf("Failed to write webservers group vars file: %v", err)
	}

	// Create host_vars directory and files
	hostVarsDir := filepath.Join(tmpDir, "host_vars")
	if err := os.MkdirAll(hostVarsDir, 0755); err != nil {
		t.Fatalf("Failed to create host_vars directory: %v", err)
	}

	web01HostFile := filepath.Join(hostVarsDir, "web01.yml")
	web01HostContent := `
server_id: 1
max_connections: 2000  # Override group var
backup_enabled: true
`
	if err := os.WriteFile(web01HostFile, []byte(web01HostContent), 0644); err != nil {
		t.Fatalf("Failed to write web01 host vars file: %v", err)
	}

	// Load inventory with paths
	inventory, err := LoadInventoryWithPaths(inventoryFile, "", tmpDir, "")
	if err != nil {
		t.Fatalf("Failed to load inventory: %v", err)
	}

	// Verify that group variables were loaded
	webserversGroup, exists := inventory.Groups["webservers"]
	if !exists {
		t.Fatal("Expected webservers group to exist")
	}

	if webserversGroup.Vars["nginx_version"] != "1.20" {
		t.Errorf("Expected nginx_version to be '1.20', got %v", webserversGroup.Vars["nginx_version"])
	}
	if webserversGroup.Vars["ssl_cert_path"] != "/etc/ssl/certs/server.crt" {
		t.Errorf("Expected ssl_cert_path to be '/etc/ssl/certs/server.crt', got %v", webserversGroup.Vars["ssl_cert_path"])
	}
	if webserversGroup.Vars["max_connections"] != 1000 {
		t.Errorf("Expected group max_connections to be 1000, got %v", webserversGroup.Vars["max_connections"])
	}

	// Verify that host variables were loaded
	web01Host, exists := inventory.Hosts["web01"]
	if !exists {
		t.Fatal("Expected web01 host to exist")
	}

	if web01Host.Vars["server_id"] != 1 {
		t.Errorf("Expected server_id to be 1, got %v", web01Host.Vars["server_id"])
	}
	if web01Host.Vars["backup_enabled"] != true {
		t.Errorf("Expected backup_enabled to be true, got %v", web01Host.Vars["backup_enabled"])
	}
	// Host vars should override group vars
	if web01Host.Vars["max_connections"] != 2000 {
		t.Errorf("Expected host max_connections to be 2000 (overriding group), got %v", web01Host.Vars["max_connections"])
	}

	// Verify GetInitialFactsForHost respects precedence
	facts := inventory.GetInitialFactsForHost(web01Host)

	// Should have global vars
	if facts["environment"] != "production" {
		t.Errorf("Expected environment to be 'production', got %v", facts["environment"])
	}

	// Should have group vars
	if facts["nginx_version"] != "1.20" {
		t.Errorf("Expected nginx_version to be '1.20', got %v", facts["nginx_version"])
	}

	// Host vars should override group vars
	if facts["max_connections"] != 2000 {
		t.Errorf("Expected max_connections to be 2000 (host override), got %v", facts["max_connections"])
	}

	// Should have host-specific vars
	if facts["backup_enabled"] != true {
		t.Errorf("Expected backup_enabled to be true, got %v", facts["backup_enabled"])
	}
}

// TestFilterInventoryByLimit tests the filterInventoryByLimit function
func TestFilterInventoryByLimit(t *testing.T) {
	// Create test inventory
	inventory := &Inventory{
		Hosts: map[string]*Host{
			"web01": {Name: "web01", Host: "192.168.1.10"},
			"web02": {Name: "web02", Host: "192.168.1.11"},
			"db01":  {Name: "db01", Host: "192.168.1.20"},
			"db02":  {Name: "db02", Host: "192.168.1.21"},
		},
		Groups: map[string]*Group{
			"webservers": {
				Hosts: map[string]*Host{
					"web01": {Name: "web01", Host: "192.168.1.10"},
					"web02": {Name: "web02", Host: "192.168.1.11"},
				},
			},
		},
		Vars: map[string]interface{}{
			"global_var": "test",
		},
	}

	tests := []struct {
		name          string
		limitPattern  string
		expectedHosts []string
		expectError   bool
	}{
		{
			name:          "exact host match",
			limitPattern:  "web01",
			expectedHosts: []string{"web01"},
			expectError:   false,
		},
		{
			name:          "wildcard pattern - single char",
			limitPattern:  "web0?",
			expectedHosts: []string{"web01", "web02"},
			expectError:   false,
		},
		{
			name:          "wildcard pattern - multi char",
			limitPattern:  "web*",
			expectedHosts: []string{"web01", "web02"},
			expectError:   false,
		},
		{
			name:          "wildcard pattern - all db",
			limitPattern:  "db*",
			expectedHosts: []string{"db01", "db02"},
			expectError:   false,
		},
		{
			name:          "multiple hosts comma separated",
			limitPattern:  "web01,db01",
			expectedHosts: []string{"web01", "db01"},
			expectError:   false,
		},
		{
			name:          "pattern with spaces",
			limitPattern:  "web01, db01",
			expectedHosts: []string{"web01", "db01"},
			expectError:   false,
		},
		{
			name:          "no matches",
			limitPattern:  "nonexistent",
			expectedHosts: []string{},
			expectError:   false,
		},
		{
			name:          "all hosts pattern",
			limitPattern:  "*",
			expectedHosts: []string{"web01", "web02", "db01", "db02"},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterInventoryByLimit(inventory, tt.limitPattern)

			if tt.expectError {
				// For this function, we don't expect errors, but test might be extended
				return
			}

			if len(result.Hosts) != len(tt.expectedHosts) {
				t.Errorf("Expected %d hosts, got %d", len(tt.expectedHosts), len(result.Hosts))
				t.Errorf("Expected hosts: %v", tt.expectedHosts)
				t.Errorf("Got hosts: %v", func() []string {
					var hosts []string
					for name := range result.Hosts {
						hosts = append(hosts, name)
					}
					return hosts
				}())
			}

			for _, expectedHost := range tt.expectedHosts {
				if _, exists := result.Hosts[expectedHost]; !exists {
					t.Errorf("Expected host %s to be in filtered inventory", expectedHost)
				}
			}

			// Verify groups are preserved (but not filtered)
			if len(result.Groups) != len(inventory.Groups) {
				t.Errorf("Expected groups to be preserved, got %d groups instead of %d", len(result.Groups), len(inventory.Groups))
			}

			// Verify vars are preserved
			if len(result.Vars) != len(inventory.Vars) {
				t.Errorf("Expected vars to be preserved, got %d vars instead of %d", len(result.Vars), len(inventory.Vars))
			}
		})
	}
}

// TestLoadInventoryWithLimitParameter tests LoadInventoryWithPaths with limit filtering
func TestLoadInventoryWithLimitParameter(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-inventory-limit-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create test inventory file
	inventoryFile := filepath.Join(tmpDir, "inventory.yml")
	inventoryContent := `
all:
  hosts:
    web01:
      host: 192.168.1.10
    web02:
      host: 192.168.1.11
    db01:
      host: 192.168.1.20
    db02:
      host: 192.168.1.21
webservers:
  hosts:
    web01: {}
    web02: {}
databases:
  hosts:
    db01: {}
    db02: {}
`
	if err := os.WriteFile(inventoryFile, []byte(inventoryContent), 0644); err != nil {
		t.Fatalf("Failed to write inventory file: %v", err)
	}

	tests := []struct {
		name          string
		inventoryFile string
		limitPattern  string
		expectedHosts []string
		expectError   bool
	}{
		{
			name:          "limit to single host",
			inventoryFile: inventoryFile,
			limitPattern:  "web01",
			expectedHosts: []string{"web01"},
			expectError:   false,
		},
		{
			name:          "limit with wildcard",
			inventoryFile: inventoryFile,
			limitPattern:  "web*",
			expectedHosts: []string{"web01", "web02"},
			expectError:   false,
		},
		{
			name:          "limit to multiple specific hosts",
			inventoryFile: inventoryFile,
			limitPattern:  "web01,db01",
			expectedHosts: []string{"web01", "db01"},
			expectError:   false,
		},
		{
			name:          "no limit - all hosts",
			inventoryFile: inventoryFile,
			limitPattern:  "",
			expectedHosts: []string{"web01", "web02", "db01", "db02"},
			expectError:   false,
		},
		{
			name:          "limit with no matches",
			inventoryFile: inventoryFile,
			limitPattern:  "nonexistent",
			expectedHosts: []string{},
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inventory, err := LoadInventoryWithPaths(tt.inventoryFile, "", tmpDir, tt.limitPattern)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(inventory.Hosts) != len(tt.expectedHosts) {
				t.Errorf("Expected %d hosts, got %d", len(tt.expectedHosts), len(inventory.Hosts))
				t.Errorf("Expected hosts: %v", tt.expectedHosts)
				t.Errorf("Got hosts: %v", func() []string {
					var hosts []string
					for name := range inventory.Hosts {
						hosts = append(hosts, name)
					}
					return hosts
				}())
			}

			for _, expectedHost := range tt.expectedHosts {
				if _, exists := inventory.Hosts[expectedHost]; !exists {
					t.Errorf("Expected host %s to be in inventory", expectedHost)
				}
			}
		})
	}
}

// TestLoadInventoryWithDefaultLocalhostLimit tests limit filtering on default localhost inventory
func TestLoadInventoryWithDefaultLocalhostLimit(t *testing.T) {
	tests := []struct {
		name         string
		limitPattern string
		expectHosts  int
		expectError  bool
	}{
		{
			name:         "limit localhost - match",
			limitPattern: "localhost",
			expectHosts:  1,
			expectError:  false,
		},
		{
			name:         "limit localhost with wildcard - match",
			limitPattern: "local*",
			expectHosts:  1,
			expectError:  false,
		},
		{
			name:         "limit nonexistent host - no match",
			limitPattern: "webserver",
			expectHosts:  0,
			expectError:  true,
		},
		{
			name:         "no limit - default localhost",
			limitPattern: "",
			expectHosts:  1,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use empty paths to trigger default localhost behavior
			inventory, err := LoadInventoryWithPaths("", "", "", tt.limitPattern)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(inventory.Hosts) != tt.expectHosts {
				t.Errorf("Expected %d hosts, got %d", tt.expectHosts, len(inventory.Hosts))
			}

			if tt.expectHosts > 0 {
				if _, exists := inventory.Hosts["localhost"]; !exists {
					t.Error("Expected localhost to exist in default inventory")
				}
			}
		})
	}
}

// TestFilterInventoryByLimitWithComplexPatterns tests complex limit patterns
func TestFilterInventoryByLimitWithComplexPatterns(t *testing.T) {
	// Create test inventory with various host naming patterns
	inventory := &Inventory{
		Hosts: map[string]*Host{
			"web-01.prod":    {Name: "web-01.prod"},
			"web-02.prod":    {Name: "web-02.prod"},
			"web-01.staging": {Name: "web-01.staging"},
			"db_primary":     {Name: "db_primary"},
			"db_replica_1":   {Name: "db_replica_1"},
			"db_replica_2":   {Name: "db_replica_2"},
			"lb01":           {Name: "lb01"},
			"cache-server":   {Name: "cache-server"},
		},
	}

	tests := []struct {
		name          string
		limitPattern  string
		expectedHosts []string
	}{
		{
			name:          "prod environment only",
			limitPattern:  "*.prod",
			expectedHosts: []string{"web-01.prod", "web-02.prod"},
		},
		{
			name:          "all web servers",
			limitPattern:  "web-*",
			expectedHosts: []string{"web-01.prod", "web-02.prod", "web-01.staging"},
		},
		{
			name:          "database servers with underscore",
			limitPattern:  "db_*",
			expectedHosts: []string{"db_primary", "db_replica_1", "db_replica_2"},
		},
		{
			name:          "multiple patterns",
			limitPattern:  "web-01.prod,db_primary,lb01",
			expectedHosts: []string{"web-01.prod", "db_primary", "lb01"},
		},
		{
			name:          "single character wildcard",
			limitPattern:  "lb0?",
			expectedHosts: []string{"lb01"},
		},
		{
			name:          "hyphenated names",
			limitPattern:  "*-server",
			expectedHosts: []string{"cache-server"},
		},
		{
			name:          "mix of exact and patterns",
			limitPattern:  "db_primary,web-*.prod",
			expectedHosts: []string{"db_primary", "web-01.prod", "web-02.prod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterInventoryByLimit(inventory, tt.limitPattern)

			if len(result.Hosts) != len(tt.expectedHosts) {
				t.Errorf("Expected %d hosts, got %d", len(tt.expectedHosts), len(result.Hosts))
				t.Errorf("Expected: %v", tt.expectedHosts)
				t.Errorf("Got: %v", func() []string {
					var hosts []string
					for name := range result.Hosts {
						hosts = append(hosts, name)
					}
					return hosts
				}())
			}

			for _, expectedHost := range tt.expectedHosts {
				if _, exists := result.Hosts[expectedHost]; !exists {
					t.Errorf("Expected host %s to be in filtered inventory", expectedHost)
				}
			}
		})
	}
}
