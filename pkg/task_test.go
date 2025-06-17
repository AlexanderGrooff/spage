package pkg

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRunOnceField(t *testing.T) {
	// Test that run_once field is properly unmarshaled from YAML
	yamlData := `---
name: test task
module: shell
run_once: true
register: test_result
`

	var task Task
	err := yaml.Unmarshal([]byte(yamlData), &task)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	if !task.RunOnce {
		t.Errorf("Expected RunOnce to be true, got %v", task.RunOnce)
	}

	if task.Name != "test task" {
		t.Errorf("Expected Name to be 'test task', got %v", task.Name)
	}

	if task.Module != "shell" {
		t.Errorf("Expected Module to be 'shell', got %v", task.Module)
	}

	if task.Register != "test_result" {
		t.Errorf("Expected Register to be 'test_result', got %v", task.Register)
	}
}

func TestRunOnceFieldDefault(t *testing.T) {
	// Test that run_once defaults to false when not specified
	yamlData := `---
name: test task
module: shell
`

	var task Task
	err := yaml.Unmarshal([]byte(yamlData), &task)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	if task.RunOnce {
		t.Errorf("Expected RunOnce to be false by default, got %v", task.RunOnce)
	}
}

func TestRunOnceToCode(t *testing.T) {
	// Test that run_once field is included in generated code
	task := Task{
		Id:       1,
		Name:     "test task",
		Module:   "shell",
		RunOnce:  true,
		Register: "test_result",
	}

	code := task.ToCode()

	// Check that RunOnce: true appears in the generated code
	if !contains(code, "RunOnce: true") {
		t.Errorf("Expected generated code to contain 'RunOnce: true', got: %s", code)
	}
}

func TestRunOnceToCodeFalse(t *testing.T) {
	// Test that run_once field is not included when false
	task := Task{
		Id:       1,
		Name:     "test task",
		Module:   "shell",
		RunOnce:  false,
		Register: "test_result",
	}

	code := task.ToCode()

	// Check that RunOnce doesn't appear when false (since it's the default)
	if contains(code, "RunOnce:") {
		t.Errorf("Expected generated code to not contain 'RunOnce:' when false, got: %s", code)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) >= 0
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
