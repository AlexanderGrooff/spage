package compile

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRolesPathFunctionality tests that roles can be found in multiple paths
func TestRolesPathFunctionality(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-rolespath-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create multiple roles directories
	roles1Dir := filepath.Join(tmpDir, "custom-roles", "role1", "tasks")
	roles2Dir := filepath.Join(tmpDir, "other-roles", "role2", "tasks")
	defaultRolesDir := filepath.Join(tmpDir, "roles", "role3", "tasks")

	// Create directory structures
	for _, dir := range []string{roles1Dir, roles2Dir, defaultRolesDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create role directory %s: %v", dir, err)
		}
	}

	// Create role task files
	role1Content := `
- name: Task from role1
  shell: echo "From role1"
`
	role2Content := `
- name: Task from role2
  shell: echo "From role2"
`
	role3Content := `
- name: Task from role3
  shell: echo "From role3"
`

	if err := os.WriteFile(filepath.Join(roles1Dir, "main.yml"), []byte(role1Content), 0644); err != nil {
		t.Fatalf("Failed to write role1 file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(roles2Dir, "main.yml"), []byte(role2Content), 0644); err != nil {
		t.Fatalf("Failed to write role2 file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(defaultRolesDir, "main.yml"), []byte(role3Content), 0644); err != nil {
		t.Fatalf("Failed to write role3 file: %v", err)
	}

	// Create a playbook that uses roles from different paths
	playbookContent := `
- name: Test playbook with multiple roles paths
  hosts: all
  roles:
    - role1
    - role2
    - role3
`
	playbookPath := filepath.Join(tmpDir, "playbook.yml")
	if err := os.WriteFile(playbookPath, []byte(playbookContent), 0644); err != nil {
		t.Fatalf("Failed to write playbook file: %v", err)
	}

	// Test with custom roles paths (colon-delimited)
	customRolesPath := filepath.Join(tmpDir, "custom-roles") + ":" + filepath.Join(tmpDir, "other-roles") + ":" + filepath.Join(tmpDir, "roles")

	// Read and preprocess the playbook with custom roles paths
	data, err := os.ReadFile(playbookPath)
	if err != nil {
		t.Fatalf("Failed to read playbook: %v", err)
	}

	// Split the roles paths
	rolesPaths := splitRolesPaths(customRolesPath)
	expectedPaths := []string{
		filepath.Join(tmpDir, "custom-roles"),
		filepath.Join(tmpDir, "other-roles"),
		filepath.Join(tmpDir, "roles"),
	}

	if len(rolesPaths) != len(expectedPaths) {
		t.Errorf("Expected %d roles paths, got %d", len(expectedPaths), len(rolesPaths))
	}

	for i, expected := range expectedPaths {
		if i >= len(rolesPaths) || rolesPaths[i] != expected {
			t.Errorf("Expected roles path %d to be %s, got %s", i, expected, rolesPaths[i])
		}
	}

	// Process the playbook with the custom roles paths
	processedBlocks, err := PreprocessPlaybook(data, tmpDir, rolesPaths)
	if err != nil {
		t.Fatalf("PreprocessPlaybook failed with custom roles paths: %v", err)
	}

	// Count tasks (excluding root block)
	taskCount := 0
	for _, block := range processedBlocks {
		if isRoot, ok := block["is_root"]; !ok || !isRoot.(bool) {
			taskCount++
		}
	}

	// Should have 3 tasks (one from each role)
	if taskCount != 3 {
		t.Errorf("Expected 3 tasks from 3 roles, got %d", taskCount)
	}

	// Verify that each role's task is present
	taskNames := make([]string, 0)
	for _, block := range processedBlocks {
		if isRoot, ok := block["is_root"]; !ok || !isRoot.(bool) {
			if name, ok := block["name"].(string); ok {
				taskNames = append(taskNames, name)
			}
		}
	}

	expectedTaskNames := []string{"Task from role1", "Task from role2", "Task from role3"}
	for _, expectedName := range expectedTaskNames {
		found := false
		for _, actualName := range taskNames {
			if actualName == expectedName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected task '%s' not found in processed tasks: %v", expectedName, taskNames)
		}
	}
}

// TestSplitRolesPaths tests the splitRolesPaths function
func TestSplitRolesPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []string{"roles"},
		},
		{
			name:     "single path",
			input:    "/opt/roles",
			expected: []string{"/opt/roles"},
		},
		{
			name:     "multiple paths",
			input:    "/opt/roles:/home/user/roles:./roles",
			expected: []string{"/opt/roles", "/home/user/roles", "./roles"},
		},
		{
			name:     "paths with spaces",
			input:    "/opt/roles : /home/user/roles : ./roles",
			expected: []string{"/opt/roles", "/home/user/roles", "./roles"},
		},
		{
			name:     "empty paths in string",
			input:    "/opt/roles::/home/user/roles:",
			expected: []string{"/opt/roles", "/home/user/roles"},
		},
		{
			name:     "only colons",
			input:    ":::",
			expected: []string{"roles"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitRolesPaths(tt.input)
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
