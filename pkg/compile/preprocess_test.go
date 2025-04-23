package compile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProcessPlaybookRoot(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a role structure for testing
	roleDir := filepath.Join(tmpDir, "roles", "testrole", "tasks")
	if err := os.MkdirAll(roleDir, 0755); err != nil {
		t.Fatalf("Failed to create role directory: %v", err)
	}

	// Create a main.yml for the role
	roleTaskContent := `
- name: Role task 1
  shell: echo "From role"
`
	if err := os.WriteFile(filepath.Join(roleDir, "main.yml"), []byte(roleTaskContent), 0644); err != nil {
		t.Fatalf("Failed to write role file: %v", err)
	}

	// Test case 1: Playbook with roles
	playbook1 := map[string]interface{}{
		"name":  "Test playbook with roles",
		"hosts": "all",
		"roles": []interface{}{"testrole"},
	}

	result1, err := processPlaybookRoot(playbook1, tmpDir)
	if err != nil {
		t.Errorf("processPlaybookRoot failed for roles: %v", err)
	}
	if len(result1) != 1 {
		t.Errorf("Expected 1 task from role, got %d", len(result1))
	}

	// Test case 2: Playbook with tasks
	playbook2 := map[string]interface{}{
		"name":  "Test playbook with tasks",
		"hosts": "all",
		"tasks": []interface{}{
			map[string]interface{}{
				"name":  "Direct task",
				"shell": "echo 'Direct'",
			},
		},
	}

	result2, err := processPlaybookRoot(playbook2, tmpDir)
	if err != nil {
		t.Errorf("processPlaybookRoot failed for tasks: %v", err)
	}
	if len(result2) != 1 {
		t.Errorf("Expected 1 direct task, got %d", len(result2))
	}

	// Test case 3: Playbook with both roles and tasks
	playbook3 := map[string]interface{}{
		"name":  "Test playbook with roles and tasks",
		"hosts": "all",
		"roles": []interface{}{"testrole"},
		"tasks": []interface{}{
			map[string]interface{}{
				"name":  "Direct task",
				"shell": "echo 'Direct'",
			},
		},
	}

	result3, err := processPlaybookRoot(playbook3, tmpDir)
	if err != nil {
		t.Errorf("processPlaybookRoot failed for roles and tasks: %v", err)
	}
	if len(result3) != 2 {
		t.Errorf("Expected 2 tasks (1 from role, 1 direct), got %d", len(result3))
	}

	// Test case 4: Invalid input - missing hosts
	playbook4 := map[string]interface{}{
		"name": "Invalid playbook",
		// Missing hosts field
		"tasks": []interface{}{
			map[string]interface{}{
				"name":  "Task",
				"shell": "echo test",
			},
		},
	}

	_, err = processPlaybookRoot(playbook4, tmpDir)
	if err == nil {
		t.Error("Expected error for missing hosts field, got nil")
	}
}

func TestPreprocessPlaybook(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a role structure for testing
	roleDir := filepath.Join(tmpDir, "roles", "testrole", "tasks")
	if err := os.MkdirAll(roleDir, 0755); err != nil {
		t.Fatalf("Failed to create role directory: %v", err)
	}

	// Create a main.yml for the role
	roleTaskContent := `
- name: Role task 1
  shell: echo "From role"
`
	if err := os.WriteFile(filepath.Join(roleDir, "main.yml"), []byte(roleTaskContent), 0644); err != nil {
		t.Fatalf("Failed to write role file: %v", err)
	}

	// Create a playbook with root level entries
	playbookContent := `
- name: Root playbook 1
  hosts: group1
  roles:
    - testrole

- name: Root playbook 2
  hosts: group2
  tasks:
    - name: Direct task
      shell: echo "Direct task"
`
	playbookPath := filepath.Join(tmpDir, "playbook.yml")
	if err := os.WriteFile(playbookPath, []byte(playbookContent), 0644); err != nil {
		t.Fatalf("Failed to write playbook file: %v", err)
	}

	// Read and preprocess the playbook
	data, err := os.ReadFile(playbookPath)
	if err != nil {
		t.Fatalf("Failed to read playbook: %v", err)
	}

	processedBlocks, err := PreprocessPlaybook(data, tmpDir)
	if err != nil {
		t.Fatalf("PreprocessPlaybook failed: %v", err)
	}

	// We expect 2 tasks total (1 from role, 1 direct)
	if len(processedBlocks) != 2 {
		t.Errorf("Expected 2 processed blocks, got %d", len(processedBlocks))
	}

	// Check first task is from the role
	task1, hasShell1 := processedBlocks[0]["shell"]
	if !hasShell1 || task1 != "echo \"From role\"" {
		t.Errorf("Expected first task to be from role, got %v", processedBlocks[0])
	}

	// Check second task is the direct task
	task2, hasShell2 := processedBlocks[1]["shell"]
	if !hasShell2 || task2 != "echo \"Direct task\"" {
		t.Errorf("Expected second task to be direct task, got %v", processedBlocks[1])
	}
}
