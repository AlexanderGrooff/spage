package compile

import (
	"os"
	"path/filepath"
	"testing"
)

// Helper to filter out the root block for task count assertions
func countTasks(blocks []map[string]interface{}) int {
	count := 0
	for _, block := range blocks {
		if isRoot, ok := block["is_root"]; !ok || !isRoot.(bool) {
			count++
		}
	}
	return count
}

func TestProcessPlaybookRoot(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

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
	if taskCount := countTasks(result1); taskCount != 1 {
		t.Errorf("Expected 1 task from role, got %d", taskCount)
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
	if taskCount := countTasks(result2); taskCount != 1 {
		t.Errorf("Expected 1 direct task, got %d", taskCount)
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
	if taskCount := countTasks(result3); taskCount != 2 {
		t.Errorf("Expected 2 tasks (1 from role, 1 direct), got %d", taskCount)
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

	// Test case 5: Playbook with pre_tasks, tasks, and post_tasks
	playbook5 := map[string]interface{}{
		"name":  "Test playbook with pre/post tasks",
		"hosts": "all",
		"pre_tasks": []interface{}{
			map[string]interface{}{
				"name":  "Pre task",
				"shell": "echo 'Pre'",
			},
		},
		"tasks": []interface{}{
			map[string]interface{}{
				"name":  "Main task",
				"shell": "echo 'Main'",
			},
		},
		"post_tasks": []interface{}{
			map[string]interface{}{
				"name":  "Post task",
				"shell": "echo 'Post'",
			},
		},
	}

	result5, err := processPlaybookRoot(playbook5, tmpDir)
	if err != nil {
		t.Errorf("processPlaybookRoot failed for pre/post tasks: %v", err)
	}
	if taskCount := countTasks(result5); taskCount != 3 {
		t.Errorf("Expected 3 tasks (1 pre, 1 main, 1 post), got %d", taskCount)
	}

	// Check order: pre_task, task, post_task
	if result5[1]["name"] != "Pre task" {
		t.Errorf("Expected first task to be pre_task, got %s", result5[1]["name"])
	}
	if result5[2]["name"] != "Main task" {
		t.Errorf("Expected second task to be main task, got %s", result5[2]["name"])
	}
	if result5[3]["name"] != "Post task" {
		t.Errorf("Expected third task to be post_task, got %s", result5[3]["name"])
	}

	// Test case 6: Playbook with roles, pre_tasks, tasks, and post_tasks
	playbook6 := map[string]interface{}{
		"name":       "Test playbook with everything",
		"hosts":      "all",
		"roles":      []interface{}{"testrole"},
		"pre_tasks":  []interface{}{map[string]interface{}{"name": "Pre task", "shell": "echo 'Pre'"}},
		"tasks":      []interface{}{map[string]interface{}{"name": "Main task", "shell": "echo 'Main'"}},
		"post_tasks": []interface{}{map[string]interface{}{"name": "Post task", "shell": "echo 'Post'"}},
	}

	result6, err := processPlaybookRoot(playbook6, tmpDir)
	if err != nil {
		t.Errorf("processPlaybookRoot failed for everything: %v", err)
	}
	if taskCount := countTasks(result6); taskCount != 4 {
		t.Errorf("Expected 4 tasks (1 role, 1 pre, 1 main, 1 post), got %d", taskCount)
	}

	// Check order: role, pre_task, task, post_task
	if result6[1]["name"] != "Role task 1" {
		t.Errorf("Expected first task to be from role, got %s", result6[1]["name"])
	}
	if result6[2]["name"] != "Pre task" {
		t.Errorf("Expected second task to be pre_task, got %s", result6[2]["name"])
	}
	if result6[3]["name"] != "Main task" {
		t.Errorf("Expected third task to be main task, got %s", result6[3]["name"])
	}
	if result6[4]["name"] != "Post task" {
		t.Errorf("Expected fourth task to be post_task, got %s", result6[4]["name"])
	}
}

func TestPreprocessPlaybook(t *testing.T) {
	// Create a temporary test directory
	tmpDir, err := os.MkdirTemp("", "spage-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

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
  pre_tasks:
    - name: Pre-task in playbook 2
      shell: echo "pre2"
  tasks:
    - name: Direct task
      shell: echo "Direct task"
  post_tasks:
    - name: Post-task in playbook 2
      shell: echo "post2"
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

	// We expect 6 blocks total:
	// Playbook 1: root block, role task (2)
	// Playbook 2: root block, pre-task, task, post-task (4)
	// Total: 2 + 4 = 6
	if len(processedBlocks) != 6 {
		t.Errorf("Expected 6 processed blocks, got %d", len(processedBlocks))
	}

	// Check tasks from playbook 1
	if name := processedBlocks[1]["name"]; name != "Role task 1" {
		t.Errorf("Expected task at index 1 to be from role, got %v", name)
	}

	// Check tasks from playbook 2 and their order
	if name := processedBlocks[3]["name"]; name != "Pre-task in playbook 2" {
		t.Errorf("Expected task at index 3 to be pre-task, got %v", name)
	}
	if name := processedBlocks[4]["name"]; name != "Direct task" {
		t.Errorf("Expected task at index 4 to be main task, got %v", name)
	}
	if name := processedBlocks[5]["name"]; name != "Post-task in playbook 2" {
		t.Errorf("Expected task at index 5 to be post-task, got %v", name)
	}
}
