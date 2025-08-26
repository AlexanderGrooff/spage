package tests

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderGrooff/spage/cmd"
	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testEnvironments = []struct {
	executor      string
	inventoryFile string
}{
	{executor: "local", inventoryFile: "inventory_localhost.yaml"},
	// {executor: "local", inventoryFile: "inventory.yaml"},
	// {executor: "temporal", inventoryFile: ""},
	// {executor: "temporal", inventoryFile: "inventory.yaml"},
}
var allowSudo = false

type playbookTestCase struct {
	playbookFile string
	configFile   string
	tags         string
	skipTags     string
	check        func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory)
	checkMode    bool
	diffMode     bool
	becomeMode   bool
}

func runPlaybookTest(t *testing.T, tc playbookTestCase) {
	t.Helper()

	for _, env := range testEnvironments {
		t.Run(fmt.Sprintf("%s_%s_%s", tc.playbookFile, env.executor, env.inventoryFile), func(t *testing.T) {
			cleanup(t)

			// 1. Load config
			configPath := ""
			if tc.configFile != "" {
				configPath = fmt.Sprintf("configs/%s", tc.configFile)
			}
			err := cmd.LoadConfig(configPath)
			require.NoError(t, err)

			cfg := cmd.GetConfig()

			// For temporal tests, generate a unique task queue to prevent interference between concurrent tests
			if env.executor == "temporal" {
				// Generate a unique task queue using test name and timestamp
				uniqueTaskQueue := fmt.Sprintf("spage-test-%s-%d",
					strings.ReplaceAll(t.Name(), "/", "-"),
					time.Now().UnixNano())
				cfg.Temporal.TaskQueue = uniqueTaskQueue
				t.Logf("Using unique task queue for temporal test: %s", uniqueTaskQueue)
			}

			if tc.checkMode {
				if cfg.Facts == nil {
					cfg.Facts = make(map[string]interface{})
				}
				cfg.Facts["ansible_check_mode"] = true
			}
			if tc.diffMode {
				if cfg.Facts == nil {
					cfg.Facts = make(map[string]interface{})
				}
				cfg.Facts["ansible_diff"] = true
			}

			// 2. Load inventory
			inventory, err := pkg.LoadInventory(env.inventoryFile, cfg)
			require.NoError(t, err, "failed to load inventory")

			// Use inventory-aware cleanup for deferred cleanup
			defer cleanupWithInventory(t, inventory)

			// 3. Get graph
			var tags, skipTags []string
			if tc.tags != "" {
				tags = strings.Split(tc.tags, ",")
			}
			if tc.skipTags != "" {
				skipTags = strings.Split(tc.skipTags, ",")
			}
			graph, err := cmd.GetGraph(tc.playbookFile, tags, skipTags, cfg, allowSudo && tc.becomeMode)
			require.NoError(t, err, "failed to get graph")

			// Capture stdout/stderr
			rescueStdout := os.Stdout
			rOut, wOut, _ := os.Pipe()
			os.Stdout = wOut

			rescueStderr := os.Stderr
			rErr, wErr, _ := os.Pipe()
			os.Stderr = wErr

			// 4. Run tasks
			var runErr error
			switch env.executor {
			case "local":
				runErr = cmd.StartLocalExecutor(&graph, env.inventoryFile, cfg, nil)
			case "temporal":
				runErr = cmd.StartTemporalExecutor(&graph, env.inventoryFile, cfg, nil)
			default:
				require.Fail(t, "invalid environment: %s", env.executor)
			}

			if err := wOut.Close(); err != nil {
				t.Logf("Failed to close stdout writer: %v", err)
			}
			if err := wErr.Close(); err != nil {
				t.Logf("Failed to close stderr writer: %v", err)
			}
			outBytes, _ := io.ReadAll(rOut)
			errBytes, _ := io.ReadAll(rErr)
			os.Stdout = rescueStdout
			os.Stderr = rescueStderr

			output := string(outBytes) + string(errBytes)

			// The in-process executor returns errors directly, not exit codes.
			// For simplicity, we'll map error presence to a non-zero exit code.
			exitCode := 0
			if runErr != nil {
				exitCode = 1
				// Also append the error to the output for checking
				output += "\n" + runErr.Error()
			}

			// 5. Check results
			if tc.check != nil {
				tc.check(t, env.executor, exitCode, output, inventory)
			}
		})
	}
}

// cleanup removes temporary files and directories created during tests.
func cleanup(t *testing.T) {
	t.Helper()
	if err := os.Remove("generated_tasks.go"); err != nil && !os.IsNotExist(err) {
		t.Logf("Failed to remove generated_tasks.go: %v", err)
	}
	if err := os.Remove("generated_tasks"); err != nil && !os.IsNotExist(err) {
		t.Logf("Failed to remove generated_tasks: %v", err)
	}

	err := os.RemoveAll("/tmp/spage")
	if err != nil && !os.IsNotExist(err) {
		t.Logf("Failed to remove /tmp/spage: %v", err)
	}
	err = os.MkdirAll("/tmp/spage", 0755)
	if err != nil {
		t.Fatalf("Failed to create /tmp/spage: %v", err)
	}

	filesToRemove := []string{
		"./test.conf",
	}
	for _, file := range filesToRemove {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			t.Logf("Failed to remove test file %s: %v", file, err)
		}
	}
}

// cleanupWithInventory removes temporary files and directories on both local and remote hosts
func cleanupWithInventory(t *testing.T, inventory *pkg.Inventory) {
	t.Helper()

	// Always do local cleanup
	cleanup(t)

	// If we have remote hosts, clean up on them too
	if isRemoteInventory(inventory) {
		host := getFirstRemoteHost(inventory)
		if host == nil {
			t.Logf("No remote host found in inventory for cleanup")
			return
		}

		hostContext := createHostContextForTesting(t, host)
		defer func() {
			if err := hostContext.Close(); err != nil {
				t.Logf("Failed to close host context: %v", err)
			}
		}()

		// Remove the /tmp/spage directory on remote host
		if _, _, _, err := hostContext.RunCommand("rm -rf /tmp/spage", ""); err != nil {
			t.Logf("Failed to remove /tmp/spage on remote host %s: %v", host.Host, err)
		}

		// Recreate the /tmp/spage directory on remote host
		if _, _, _, err := hostContext.RunCommand("mkdir -p /tmp/spage", ""); err != nil {
			t.Logf("Failed to create /tmp/spage on remote host %s: %v", host.Host, err)
		}

		// Remove specific test files on remote host
		remoteFilesToRemove := []string{
			"./test.conf",
		}
		for _, file := range remoteFilesToRemove {
			if _, _, _, err := hostContext.RunCommand(fmt.Sprintf("rm -f %s", file), ""); err != nil {
				t.Logf("Failed to remove test file %s on remote host %s: %v", file, host.Host, err)
			}
		}

		t.Logf("Completed cleanup on remote host %s", host.Host)
	}
}

// Helper function to check for the existence of a file.
func assertFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.NoError(t, err, "Expected file to exist: %s", path)
}

// Helper function to check for the existence of a directory.
func assertDirectoryExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err, "Expected directory to exist: %s", path)
	assert.True(t, info.IsDir(), "Expected path to be a directory: %s", path)
}

// Helper function to check that a file does not exist.
func assertFileDoesNotExist(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "Expected file to not exist: %s", path)
}

// Helper function to check for the content of a file.
func assertFileContains(t *testing.T, path, expectedContent string) {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err, "Failed to read file: %s", path)
	assert.Contains(t, string(content), expectedContent, "File content mismatch: %s", path)
}

// Helper function to check that a file does not contain a specific string.
func assertFileDoesNotContain(t *testing.T, path, unexpectedContent string) {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err, "Failed to read file: %s", path)
	assert.NotContains(t, string(content), unexpectedContent, "File content mismatch: %s", path)
}

// Remote-aware assertion helpers that work with inventory context
func assertFileExistsWithInventory(t *testing.T, path string, inventory *pkg.Inventory) {
	t.Helper()
	if isRemoteInventory(inventory) {
		// Check on the first remote host
		host := getFirstRemoteHost(inventory)
		if host == nil {
			t.Fatalf("No remote host found in inventory for checking file: %s", path)
			return
		}
		hostContext := createHostContextForTesting(t, host)
		defer func() {
			if err := hostContext.Close(); err != nil {
				t.Logf("Failed to close host context: %v", err)
			}
		}()

		_, err := hostContext.Stat(path, false)
		assert.NoError(t, err, "Expected file to exist on remote host %s: %s", host.Host, path)
	} else {
		assertFileExists(t, path)
	}
}

func assertDirectoryExistsWithInventory(t *testing.T, path string, inventory *pkg.Inventory) {
	t.Helper()
	if isRemoteInventory(inventory) {
		// Check on the first remote host
		host := getFirstRemoteHost(inventory)
		if host == nil {
			t.Fatalf("No remote host found in inventory for checking directory: %s", path)
			return
		}
		hostContext := createHostContextForTesting(t, host)
		defer func() {
			if err := hostContext.Close(); err != nil {
				t.Logf("Failed to close host context: %v", err)
			}
		}()

		info, err := hostContext.Stat(path, false)
		require.NoError(t, err, "Expected directory to exist on remote host %s: %s", host.Host, path)
		assert.True(t, info.IsDir(), "Expected path to be a directory on remote host %s: %s", host.Host, path)
	} else {
		assertDirectoryExists(t, path)
	}
}

func assertFileDoesNotExistWithInventory(t *testing.T, path string, inventory *pkg.Inventory) {
	t.Helper()
	if isRemoteInventory(inventory) {
		// Check on the first remote host
		host := getFirstRemoteHost(inventory)
		if host == nil {
			t.Fatalf("No remote host found in inventory for checking file: %s", path)
			return
		}
		hostContext := createHostContextForTesting(t, host)
		defer func() {
			if err := hostContext.Close(); err != nil {
				t.Logf("Failed to close host context: %v", err)
			}
		}()

		_, err := hostContext.Stat(path, false)
		assert.True(t, os.IsNotExist(err), "Expected file to not exist on remote host %s: %s", host.Host, path)
	} else {
		assertFileDoesNotExist(t, path)
	}
}

func assertFileContainsWithInventory(t *testing.T, path, expectedContent string, inventory *pkg.Inventory) {
	t.Helper()
	if isRemoteInventory(inventory) {
		// Check on the first remote host
		host := getFirstRemoteHost(inventory)
		if host == nil {
			t.Fatalf("No remote host found in inventory for checking file content: %s", path)
			return
		}
		hostContext := createHostContextForTesting(t, host)
		defer func() {
			if err := hostContext.Close(); err != nil {
				t.Logf("Failed to close host context: %v", err)
			}
		}()

		content, err := hostContext.ReadFile(path, "")
		require.NoError(t, err, "Failed to read file on remote host %s: %s", host.Host, path)
		assert.Contains(t, content, expectedContent, "File content mismatch on remote host %s: %s", host.Host, path)
	} else {
		assertFileContains(t, path, expectedContent)
	}
}

func assertFileDoesNotContainWithInventory(t *testing.T, path, unexpectedContent string, inventory *pkg.Inventory) {
	t.Helper()
	if isRemoteInventory(inventory) {
		// Check on the first remote host
		host := getFirstRemoteHost(inventory)
		if host == nil {
			t.Fatalf("No remote host found in inventory for checking file content: %s", path)
			return
		}
		hostContext := createHostContextForTesting(t, host)
		defer func() {
			if err := hostContext.Close(); err != nil {
				t.Logf("Failed to close host context: %v", err)
			}
		}()

		content, err := hostContext.ReadFile(path, "")
		require.NoError(t, err, "Failed to read file on remote host %s: %s", host.Host, path)
		assert.NotContains(t, content, unexpectedContent, "File content mismatch on remote host %s: %s", host.Host, path)
	} else {
		assertFileDoesNotContain(t, path, unexpectedContent)
	}
}

// Helper functions for remote inventory checking
func isRemoteInventory(inventory *pkg.Inventory) bool {
	if inventory == nil || len(inventory.Hosts) == 0 {
		return false
	}
	// Check if any host is not local
	for _, host := range inventory.Hosts {
		if !host.IsLocal {
			return true
		}
	}
	return false
}

func getFirstRemoteHost(inventory *pkg.Inventory) *pkg.Host {
	if inventory == nil {
		return nil
	}
	for _, host := range inventory.Hosts {
		if !host.IsLocal {
			return host
		}
	}
	return nil
}

func createHostContextForTesting(t *testing.T, host *pkg.Host) *pkg.HostContext {
	t.Helper()
	// Create a minimal config for testing
	cfg := &config.Config{
		HostKeyChecking: false, // Disable for testing
	}

	hostContext, err := pkg.InitializeHostContext(host, cfg)
	require.NoError(t, err, "Failed to initialize host context for testing on host: %s", host.Host)
	return hostContext
}

func TestVarsPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/vars_playbook.yaml",
		configFile:   "sequential.yaml", // Use sequential to ensure proper order
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "vars_playbook should run without error in env: %s, output: %s", envName, output)

			// Test 1: Play-level variables
			assertFileExistsWithInventory(t, "/tmp/spage/play_level_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/play_level_test.txt", "play level value", inventory)

			// Test 2: Templated variables (should contain inventory hostname)
			assertFileExistsWithInventory(t, "/tmp/spage/templated_test.txt", inventory)
			// Check for the appropriate hostname based on inventory type
			if isRemoteInventory(inventory) {
				assertFileContainsWithInventory(t, "/tmp/spage/templated_test.txt", "templated theta", inventory)
			} else {
				assertFileContainsWithInventory(t, "/tmp/spage/templated_test.txt", "templated localhost", inventory)
			}

			// Test 3: Task-level variables override play-level
			assertFileExistsWithInventory(t, "/tmp/spage/task_override_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/task_override_test.txt", "task level override", inventory)

			// Test 4: Task-level only variables
			assertFileExistsWithInventory(t, "/tmp/spage/task_only_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/task_only_test.txt", "task only value", inventory)

			// Test 5: Variables in templates
			assertFileExistsWithInventory(t, "/tmp/spage/vars_template_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/vars_template_test.txt", "variables in templates work", inventory)

			// Test 6: set_fact variables
			assertFileExistsWithInventory(t, "/tmp/spage/fact_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/fact_test.txt", "fact value", inventory)

			assertFileExistsWithInventory(t, "/tmp/spage/computed_fact_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/computed_fact_test.txt", "play level value computed", inventory)

			// Test 7: register variables
			assertFileExistsWithInventory(t, "/tmp/spage/registered_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/registered_test.txt", "registered output", inventory)

			// Test 8: Variables in when conditions
			assertFileExistsWithInventory(t, "/tmp/spage/when_boolean_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/when_boolean_test.txt", "boolean condition works", inventory)
			// This file should NOT exist because when: not boolean_var (boolean_var is true)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/when_boolean_false_test.txt", inventory)

			// Test 9: Variables in loops
			assertFileExistsWithInventory(t, "/tmp/spage/loop_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/loop_test.txt", "item1", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/loop_test.txt", "item2", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/loop_test.txt", "item3", inventory)

			// Test 10: Dictionary variable access
			assertFileExistsWithInventory(t, "/tmp/spage/dict_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/dict_test.txt", "value1", inventory)

			// Test 11: Nested dictionary access
			assertFileExistsWithInventory(t, "/tmp/spage/nested_dict_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/nested_dict_test.txt", "deep_value", inventory)

			// Test 11: String operations and filters
			assertFileExistsWithInventory(t, "/tmp/spage/string_ops_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/string_ops_test.txt", "PLAY LEVEL VALUE", inventory)

			// Test 12: Default values for undefined variables
			assertFileExistsWithInventory(t, "/tmp/spage/default_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/default_test.txt", "default value", inventory)

			// Test 13: Variable precedence
			assertFileExistsWithInventory(t, "/tmp/spage/precedence_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/precedence_test.txt", "task wins", inventory)

			// Test 14: Mathematical operations (simplified)
			assertFileExistsWithInventory(t, "/tmp/spage/math_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/math_test.txt", "50", inventory)

			// Test 15: Play isolation (second play) - TODO: Currently Spage merges plays, fix framework
			assertFileExistsWithInventory(t, "/tmp/spage/play_isolation_test.txt", inventory)
			// TODO: Should be "second play value" when multi-play isolation is implemented
			assertFileContainsWithInventory(t, "/tmp/spage/play_isolation_test.txt", "play level value", inventory)

			// Test 16: Fact isolation across plays - TODO: Currently facts persist across plays
			assertFileExistsWithInventory(t, "/tmp/spage/fact_isolation_test.txt", inventory)
			// TODO: Should be "fact not available" when play isolation is implemented
			assertFileContainsWithInventory(t, "/tmp/spage/fact_isolation_test.txt", "fact value", inventory)
		},
	})
}
func TestTemplatePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/template_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "template_playbook should run without error in env: %s, output: %s", envName, output)
			// ./test.conf is intentionally removed by the "remove relative dst file" task
			assertFileDoesNotExistWithInventory(t, "./test.conf", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/test.conf", inventory)
		},
	})
}

func TestJinjaIncludePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/jinja_include_playbook.yaml",
		configFile:   "sequential_no_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "jinja_include_playbook should run without error in env: %s, output: %s", envName, output)

			// Ensure the parent rendered file exists and contains both header, child content, and footer
			target := "/tmp/spage/jinja_include_rendered.txt"
			assertFileExistsWithInventory(t, target, inventory)
		},
	})
}

func TestShellPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/shell_playbook.yaml",
		configFile:   "parallel_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "shell_playbook should run without error in env: %s, output: %s", envName, output)
			assertDirectoryExistsWithInventory(t, "/tmp/spage/spage_test", inventory)
		},
	})
}

func TestFilePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/file_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "file_playbook should run without error in env: %s, output: %s", envName, output)
			assertFileExistsWithInventory(t, "/tmp/spage/spage_test_file.txt", inventory)
		},
	})
}

func TestInvalidPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/invalid_playbook.yaml",
		configFile:   "parallel_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.NotEqual(t, 0, exitCode, "invalid_playbook should fail in env: %s", envName)
		},
	})
}

func TestRevertPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/revert_playbook.yaml",
		configFile:   "parallel_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 1, exitCode, "revert_playbook should fail with exit code 1 in env: %s", envName)
			assertFileContainsWithInventory(t, "/tmp/spage/revert_test.txt", "reverted", inventory)
		},
	})
}

func TestRevertPlaybookNoRevert(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/revert_playbook.yaml",
		configFile:   "no_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 1, exitCode, "revert_playbook with no_revert should fail with exit code 1 in env: %s", envName)
			assertFileContainsWithInventory(t, "/tmp/spage/revert_test.txt", "not reverted", inventory)
		},
	})
}

func TestExecutionModePlaybookParallelFails(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/execution_mode_playbook.yaml",
		configFile:   "parallel_revert.yaml", // default is parallel
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.NotEqual(t, 0, exitCode, "execution_mode_playbook in parallel should fail in env: %s", envName)
		},
	})
}

func TestExecutionModePlaybookSequentialSucceeds(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/execution_mode_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "execution_mode_playbook in sequential should succeed in env: %s, output: %s", envName, output)
			path := "/tmp/spage/exec_mode_test.txt"
			assertFileContainsWithInventory(t, path, "step3", inventory)
		},
	})
}

func TestIncludePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/include_playbook.yaml",
		configFile:   "parallel_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "include_playbook should succeed in env: %s, output: %s", envName, output)
			assertFileExistsWithInventory(t, "/tmp/spage/include_test_main_start.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/include_test_included.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/include_test_main_end.txt", inventory)
		},
	})
}

func TestIncludeRolePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/include_role_playbook.yaml",
		configFile:   "parallel_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "include_role_playbook should succeed in env: %s, output: %s", envName, output)
			assertFileExistsWithInventory(t, "/tmp/spage/include_role_before.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/include_role_task.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/include_role_after.txt", inventory)
		},
	})
}

func TestImportTasksPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/import_tasks_playbook.yaml",
		configFile:   "parallel_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "import_tasks_playbook should succeed in env: %s, output: %s", envName, output)
			assertFileExistsWithInventory(t, "/tmp/spage/import_tasks_before.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/import_tasks_imported.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/import_tasks_after.txt", inventory)
		},
	})
}

func TestImportRolePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/import_role_playbook.yaml",
		configFile:   "parallel_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "import_role_playbook should succeed in env: %s, output: %s", envName, output)
			assertFileExistsWithInventory(t, "/tmp/spage/import_role_before.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/include_role_task.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/import_role_after.txt", inventory)
		},
	})
}

func TestAssertPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/assert_playbook.yaml",
		configFile:   "parallel_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.NotEqual(t, 0, exitCode, "assert_playbook should fail in env: %s", envName)
		},
	})
}

func TestRootTasksPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/root_tasks_playbook.yaml",
		configFile:   "parallel_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "root_tasks_playbook should succeed")
			assertFileContainsWithInventory(t, "/tmp/spage/root_playbook_tasks.txt", "Created by root-level tasks section", inventory)
		},
	})
}

func TestRootRolesPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/root_roles_playbook.yaml",
		configFile:   "parallel_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "root_roles_playbook should succeed")
			assertFileContainsWithInventory(t, "/tmp/spage/root_playbook_role.txt", "Created by root-level roles section", inventory)
		},
	})
}

func TestRootBothPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/root_both_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "root_both_playbook should succeed")
			assertFileContainsWithInventory(t, "/tmp/spage/root_playbook_role.txt", "Created by root-level roles section", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/root_playbook_role.txt", "Additional task", inventory)
		},
	})
}

func TestStatPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/stat_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "stat_playbook should succeed")
		},
	})
}

func TestCommandPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/command_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.NotEqual(t, 0, exitCode, "command_playbook should fail to test revert")
			assertFileExistsWithInventory(t, "/tmp/spage/spage_command_test_file1.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/spage_command_test_file2.txt", inventory)
			assertDirectoryExistsWithInventory(t, "/tmp/spage/spage_command_dir", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/spage_command_test_no_expand.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/spage_command_revert.txt", inventory)
		},
	})
}

func TestWhenPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/when_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "when_playbook should succeed")
			assertFileExistsWithInventory(t, "/tmp/spage/spage_when_test_should_run.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/spage_when_test_simple_true.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/spage_when_test_should_skip.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/spage_when_test_nonexistent.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/spage_when_test_simple_false.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/spage_when_test_multiple_conditions.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/spage_when_test_multiple_false_conditions.txt", inventory)
		},
	})
}

func TestSlurpPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/slurp_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "slurp_playbook should succeed")
		},
	})
}

func TestLoopSequentialPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/loop_sequential_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "loop_sequential_playbook should succeed")
		},
	})
}

func TestAnsibleBuiltinPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/ansible_builtin_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "ansible_builtin_playbook should succeed")
		},
	})
}

func TestConnectionLocalPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/connection_local_playbook.yaml",
		configFile:   "sequential_no_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "connection_local_playbook should succeed in env: %s, output: %s", envName, output)
			assertFileExistsWithInventory(t, "/tmp/spage/connection_local_test.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/connection_local_test.txt", "local connection works", inventory)
		},
	})
}

func TestFailModulePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/fail_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.NotEqual(t, 0, exitCode, "fail_module_playbook should fail")
			assertFileContainsWithInventory(t, "/tmp/spage/fail_test.txt", "About to test the fail module", inventory)
			assertFileDoesNotContainWithInventory(t, "/tmp/spage/fail_test.txt", "This message should never appear", inventory)
		},
	})
}

func TestLineinfilePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/lineinfile_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "lineinfile_playbook should succeed")
			path := "/tmp/spage/lineinfile_test.txt"
			assertFileContainsWithInventory(t, path, "First line, added with insertbefore BOF.", inventory)
			assertFileContainsWithInventory(t, path, "Spage still is awesome, indeed!", inventory)
			assertFileContainsWithInventory(t, path, "This is a new line without regexp.", inventory)
			assertFileContainsWithInventory(t, path, "Last line, added with insertafter EOF.", inventory)
			assertFileDoesNotContainWithInventory(t, path, "Spage was here!", inventory)
			assertFileDoesNotContainWithInventory(t, path, "# This is a comment to remove", inventory)
			assertFileDoesNotContainWithInventory(t, path, "This line will be removed by exact match.", inventory)

			templatedPath := "/tmp/spage/lineinfile_templated.txt"
			assertFileExistsWithInventory(t, templatedPath, inventory)
			assertFileContainsWithInventory(t, templatedPath, "This line comes from a set_fact variable.", inventory)
		},
	})
}

func TestLineinfileRevertPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/lineinfile_revert_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.NotEqual(t, 0, exitCode, "lineinfile_revert_playbook should fail")
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/lineinfile_revert_specific.txt", inventory)
		},
	})
}

func TestDelegateToPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/delegate_to_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "delegate_to_playbook should succeed")
			// All files are on the local machine because it does `delegate_to: localhost`
			assertFileExists(t, "/tmp/spage/delegate_test_on_localhost.txt")
			assertFileExistsWithInventory(t, "/tmp/spage/delegate_test_on_inventory_host.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/delegate_test_delegated_to_inventory_host.txt", inventory)
		},
	})
}

func TestFactGatheringPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/fact_gathering_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "fact_gathering_playbook should succeed")
		},
	})
}

func TestRunOncePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/run_once_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "run_once_playbook should succeed")
			// Assuming local run, all files are on the local machine.
			assertFileExistsWithInventory(t, "/tmp/spage/run_once_test.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/normal_task.txt", inventory)

			loopFile := "/tmp/spage/run_once_loop.txt"
			assertFileExistsWithInventory(t, loopFile, inventory)
			assertFileContainsWithInventory(t, loopFile, "loop_item_first", inventory)
			assertFileContainsWithInventory(t, loopFile, "loop_item_second", inventory)
			assertFileContainsWithInventory(t, loopFile, "loop_item_third", inventory)
		},
	})
}

func TestUntilLoopPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/until_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "until_loop_playbook should succeed")
			assertFileExistsWithInventory(t, "/tmp/spage/until_succeeded.txt", inventory)
		},
	})
}

func TestTagsConfig(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/tags_playbook.yaml",
		configFile:   "sequential.yaml",
		tags:         "config",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "tags_config should succeed")
			// Tasks that should run with 'config' tag
			assertFileExistsWithInventory(t, "/tmp/spage/tag_test_single_config.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/tag_test_config_deploy.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/tag_test_always.txt", inventory) // 'always' tag runs with any tag selection
			// Tasks that should NOT run
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_no_tags.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_multiple_database_setup.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_deploy.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_skip.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_never.txt", inventory)
		},
	})
}

func TestSkipTagsSkip(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/tags_playbook.yaml",
		configFile:   "sequential.yaml",
		skipTags:     "skip",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "skip_tags_skip should succeed")
			// Tasks that should run (all except 'skip' and 'never')
			assertFileExistsWithInventory(t, "/tmp/spage/tag_test_no_tags.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/tag_test_single_config.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/tag_test_multiple_database_setup.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/tag_test_always.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/tag_test_deploy.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/tag_test_config_deploy.txt", inventory)
			// Tasks that should NOT run
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_skip.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_never.txt", inventory) // 'never' doesn't run by default
		},
	})
}

func TestTagsNever(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/tags_playbook.yaml",
		configFile:   "sequential.yaml",
		tags:         "never",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "tags_never should succeed")
			// When explicitly selecting 'never' tag, only 'never' and 'always' tasks should run
			assertFileExistsWithInventory(t, "/tmp/spage/tag_test_never.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/tag_test_always.txt", inventory)
			// All other tasks should NOT run
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_no_tags.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_single_config.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_multiple_database_setup.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_deploy.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_config_deploy.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/tag_test_skip.txt", inventory)
		},
	})
}

func TestPythonFallbackModule(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/python_fallback_module.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "python_fallback_module should succeed")
			// Verify that Python fallback worked by checking the files created
			assertFileExistsWithInventory(t, "/tmp/spage/python_fallback_ping.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/python_fallback_ping.txt", "pong", inventory)
		},
	})
}

func TestCheckMode(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/check_mode_playbook.yaml",
		configFile:   "sequential.yaml",
		checkMode:    true,
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "check_mode should succeed")
		},
	})
}

func TestDiffMode(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/diff_mode_playbook.yaml",
		configFile:   "sequential.yaml",
		diffMode:     true,
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "diff_mode should succeed")
			// Verify that all diff operations completed successfully
			assertFileExistsWithInventory(t, "/tmp/spage/diff_mode_no_keyword.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/diff_mode_no_keyword.txt", "no_diff_keyword_completed", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/diff_mode_yes.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/diff_mode_yes.txt", "diff_yes_completed", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/diff_mode_no.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/diff_mode_no.txt", "diff_no_completed", inventory)

			// Verify that the lineinfile operations actually worked by checking file contents
			assertFileExistsWithInventory(t, "/tmp/spage/diff_test_file_1.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/diff_test_file_1.txt", "changed content 1", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/diff_test_file_2.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/diff_test_file_2.txt", "changed content 2", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/diff_test_file_3.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/diff_test_file_3.txt", "changed content 3", inventory)
		},
	})
}

func TestTemplateDiffMode(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/template_diff_test.yaml",
		configFile:   "sequential.yaml",
		diffMode:     true,
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "template_diff_mode should succeed")
			// Verify that all template operations completed successfully
			assertFileExistsWithInventory(t, "/tmp/spage/template_diff_no_keyword.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/template_diff_no_keyword.txt", "no_diff_keyword_completed", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/template_diff_yes.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/template_diff_yes.txt", "diff_yes_completed", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/template_diff_no.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/template_diff_no.txt", "diff_no_completed", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/template_diff_different.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/template_diff_different.txt", "different_content_completed", inventory)

			// TODO: improve testing
		},
	})
}

func TestHandlersPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/handlers_playbook.yaml",
		configFile:   "sequential_no_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "handlers_playbook should succeed in env: %s, output: %s", envName, output)

			// Verify that regular tasks ran
			assertFileExistsWithInventory(t, "/tmp/spage/handlers_regular_task1.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/handlers_regular_task2.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/handlers_regular_task3.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/handlers_regular_task4.txt", inventory)

			// Verify that handlers were executed after regular tasks
			assertFileExistsWithInventory(t, "/tmp/spage/handlers_handler1.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/handlers_handler2.txt", inventory)

			// Handler3 should NOT exist because it was never notified
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/handlers_handler3.txt", inventory)

			// Verify that handlers ran only once (handler1 was notified twice but should only run once)
			assertFileContainsWithInventory(t, "/tmp/spage/handlers_handler1.txt", "handler1 executed", inventory)
			// The file should not contain multiple executions - check this inventory-aware
			var content string
			var err error
			if isRemoteInventory(inventory) {
				host := getFirstRemoteHost(inventory)
				if host != nil {
					hostContext := createHostContextForTesting(t, host)
					defer func() {
						if err := hostContext.Close(); err != nil {
							t.Logf("Failed to close host context: %v", err)
						}
					}()
					content, err = hostContext.ReadFile("/tmp/spage/handlers_handler1.txt", "")
				}
			} else {
				contentBytes, readErr := os.ReadFile("/tmp/spage/handlers_handler1.txt")
				if readErr == nil {
					content = string(contentBytes)
				}
				err = readErr
			}

			if err == nil {
				lines := strings.Split(content, "\n")
				count := 0
				for _, line := range lines {
					if strings.Contains(line, "handler1 executed") {
						count++
					}
				}
				assert.Equal(t, 1, count, "Handler1 should only execute once even when notified multiple times")
			}
		},
	})
}

func TestHandlersWithFailedTasksPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/handlers_failed_tasks_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.NotEqual(t, 0, exitCode, "handlers_failed_tasks_playbook should fail in env: %s", envName)

			// Verify that the successful task ran and its handler executed
			assertFileExistsWithInventory(t, "/tmp/spage/handlers_success_task.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/handlers_success_handler.txt", inventory)

			// Verify that the failed task created its file but its handler did NOT run
			assertFileExistsWithInventory(t, "/tmp/spage/handlers_failed_task.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/handlers_failed_handler.txt", inventory)

			// Verify that tasks after the failed task did not run
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/handlers_after_failed_task.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/handlers_after_failed_handler.txt", inventory)
		},
	})
}

func TestShorthandSyntaxPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/shorthand_syntax_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "shorthand_syntax_playbook should succeed in env: %s, output: %s", envName, output)

			// Verify that shorthand template syntax works (parsing and file creation)
			assertFileExistsWithInventory(t, "/tmp/spage/shorthand_template.conf", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/shorthand_template.conf", "some content here", inventory)

			// Verify that regular template syntax works (comparison)
			assertFileExistsWithInventory(t, "/tmp/spage/regular_template.conf", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/regular_template.conf", "some content here", inventory)

			// Verify that shorthand shell syntax works
			assertFileExistsWithInventory(t, "/tmp/spage/shorthand_shell.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/shorthand_shell.txt", "shorthand shell test", inventory)

			// Verify that regular shell syntax works (comparison)
			assertFileExistsWithInventory(t, "/tmp/spage/regular_shell.txt", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/regular_shell.txt", "regular shell test", inventory)

			// Verify that shorthand command syntax works (creates empty files via touch)
			assertFileExistsWithInventory(t, "/tmp/spage/shorthand_command.txt", inventory)

			// Verify that regular command syntax works (comparison)
			assertFileExistsWithInventory(t, "/tmp/spage/regular_command.txt", inventory)

			// Verify that shorthand syntax with quoted values works
			assertFileExistsWithInventory(t, "/tmp/spage/shorthand_quoted.conf", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/shorthand_quoted.conf", "some content here", inventory)

			// Verify that shorthand syntax with multiple parameters works
			assertFileExistsWithInventory(t, "/tmp/spage/shorthand_multi.conf", inventory)
			assertFileContainsWithInventory(t, "/tmp/spage/shorthand_multi.conf", "some content here", inventory)

			// Verify that template files have identical content regardless of syntax style
			if !isRemoteInventory(inventory) {
				// For local execution, we can compare file contents directly
				shorthandContent, err := os.ReadFile("/tmp/spage/shorthand_template.conf")
				assert.NoError(t, err, "Should be able to read shorthand template file")

				regularContent, err := os.ReadFile("/tmp/spage/regular_template.conf")
				assert.NoError(t, err, "Should be able to read regular template file")

				quotedContent, err := os.ReadFile("/tmp/spage/shorthand_quoted.conf")
				assert.NoError(t, err, "Should be able to read quoted template file")

				multiContent, err := os.ReadFile("/tmp/spage/shorthand_multi.conf")
				assert.NoError(t, err, "Should be able to read multi parameter template file")

				// All template files should have the same content
				expectedContent := "some content here\n"
				assert.Equal(t, expectedContent, string(shorthandContent), "Shorthand template should have correct content")
				assert.Equal(t, expectedContent, string(regularContent), "Regular template should have correct content")
				assert.Equal(t, expectedContent, string(quotedContent), "Quoted template should have correct content")
				assert.Equal(t, expectedContent, string(multiContent), "Multi parameter template should have correct content")

				// Verify that shorthand and regular syntax produce identical results
				assert.Equal(t, string(shorthandContent), string(regularContent), "Shorthand and regular template syntax should produce identical content")
			} else {
				// For remote execution, use the inventory-aware content check
				host := getFirstRemoteHost(inventory)
				if host != nil {
					hostContext := createHostContextForTesting(t, host)
					defer func() {
						if err := hostContext.Close(); err != nil {
							t.Logf("Failed to close host context: %v", err)
						}
					}()

					shorthandContent, err := hostContext.ReadFile("/tmp/spage/shorthand_template.conf", "")
					assert.NoError(t, err, "Should be able to read shorthand template file on remote host")

					regularContent, err := hostContext.ReadFile("/tmp/spage/regular_template.conf", "")
					assert.NoError(t, err, "Should be able to read regular template file on remote host")

					expectedContent := "some content here\n"
					assert.Equal(t, expectedContent, shorthandContent, "Shorthand template should have correct content on remote host")
					assert.Equal(t, expectedContent, regularContent, "Regular template should have correct content on remote host")
					assert.Equal(t, shorthandContent, regularContent, "Shorthand and regular template syntax should produce identical content on remote host")
				}
			}
		},
	})
}

func TestLocalActionPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/local_action_playbook.yaml",
		configFile:   "sequential_no_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "Expected exit code 0, got %d. Output: %s", exitCode, output)

			// local_action should always execute on the local machine, so we don't use assertFile...WithInventory
			assertFileExists(t, "/tmp/spage/local_action_test.txt")
			assertFileContains(t, "/tmp/spage/local_action_test.txt", "hello from local_action")

			assertFileExists(t, "/tmp/spage/local_action_test_args.txt")
			assertFileContains(t, "/tmp/spage/local_action_test_args.txt", "local_action module args")
		},
	})
}

func TestFailedWhenPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/failed_when_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.NotEqual(t, 0, exitCode, "failed_when_playbook should fail")
			assertFileExistsWithInventory(t, "/tmp/spage/failed_when_succeed.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/failed_when_ignore.txt", inventory)
			assertFileExistsWithInventory(t, "/tmp/spage/failed_when_fail.txt", inventory)
			assertFileDoesNotExistWithInventory(t, "/tmp/spage/failed_when_after_actual_fail.txt", inventory)
		},
	})
}

func TestJinjaTestsPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/jinja_tests_playbook.yaml",
		configFile:   "sequential_no_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "jinja_tests_playbook should succeed")
		},
	})
}

func TestNestedDirPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/nested/playbooks/nested_dir_playbook.yaml",
		configFile:   "sequential_no_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "nested_dir_playbook should succeed")
		},
	})
}

func TestBecomeModePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/become_mode_playbook.yaml",
		configFile:   "sequential_no_revert.yaml",
		becomeMode:   true,
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			if !allowSudo {
				assert.Equal(t, 1, exitCode, "become_mode_playbook should fail")
				return
			}
			assert.Equal(t, 0, exitCode, "become_mode_playbook should succeed")
		},
	})
}

func TestGroupVarsPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/group_vars_playbook.yaml",
		configFile:   "sequential_no_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "group_vars_playbook should succeed")
		},
	})
}

func TestHostVarsPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/host_vars_playbook.yaml",
		configFile:   "sequential_no_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "host_vars_playbook should succeed")
		},
	})
}

func TestOverwritingVariablesPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/overwriting_variables.yaml",
		configFile:   "sequential_no_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "overwriting_variables should succeed")
		},
	})
}
