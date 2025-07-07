package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexanderGrooff/spage/cmd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type playbookTestCase struct {
	playbookFile string
	configFile   string
	tags         string
	skipTags     string
	check        func(t *testing.T, envName string, exitCode int, output string)
	checkMode    bool
	diffMode     bool
}

func runPlaybookTest(t *testing.T, tc playbookTestCase) {
	t.Helper()
	environments := []struct {
		executor      string
		inventoryFile string
	}{
		{executor: "local", inventoryFile: ""},
		{executor: "temporal", inventoryFile: ""},
	}

	for _, env := range environments {
		t.Run(fmt.Sprintf("%s_%s_%s", tc.playbookFile, env.executor, env.inventoryFile), func(t *testing.T) {
			cleanup(t)
			defer cleanup(t)

			// 1. Load config
			configPath := ""
			if tc.configFile != "" {
				configPath = fmt.Sprintf("configs/%s", tc.configFile)
			}
			err := cmd.LoadConfig(configPath)
			require.NoError(t, err)

			cfg := cmd.GetConfig()
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

			// 2. Get graph
			var tags, skipTags []string
			if tc.tags != "" {
				tags = strings.Split(tc.tags, ",")
			}
			if tc.skipTags != "" {
				skipTags = strings.Split(tc.skipTags, ",")
			}
			graph, err := cmd.GetGraph(tc.playbookFile, tags, skipTags, cfg)
			require.NoError(t, err, "failed to get graph")

			// Capture stdout/stderr
			rescueStdout := os.Stdout
			rOut, wOut, _ := os.Pipe()
			os.Stdout = wOut

			rescueStderr := os.Stderr
			rErr, wErr, _ := os.Pipe()
			os.Stderr = wErr

			// 3. Run tasks
			var runErr error
			switch env.executor {
			case "local":
				runErr = cmd.StartLocalExecutor(graph, env.inventoryFile, cfg)
			case "temporal":
				runErr = cmd.StartTemporalExecutor(graph, env.inventoryFile, cfg)
			default:
				require.Fail(t, "invalid environment: %s", env.executor)
			}

			wOut.Close()
			wErr.Close()
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

			// 4. Check results
			if tc.check != nil {
				tc.check(t, env.executor, exitCode, output)
			}
		})
	}
}

// cleanup removes temporary files and directories created during tests.
func cleanup(t *testing.T) {
	t.Helper()
	os.Remove("generated_tasks.go")
	os.Remove("generated_tasks")

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

func createTestPlaybook(t *testing.T, name, content string) string {
	t.Helper()
	// assumes the test is run from the tests directory
	playbooksDir := "playbooks"
	if _, err := os.Stat(playbooksDir); os.IsNotExist(err) {
		err = os.Mkdir(playbooksDir, 0755)
		require.NoError(t, err)
	}
	path := filepath.Join(playbooksDir, name)
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
	t.Cleanup(func() {
		os.Remove(path)
	})
	return path
}

func TestVarsPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/vars_playbook.yaml",
		configFile:   "default.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "vars_playbook should run without error in env: %s, output: %s", envName, output)
		},
	})
}
func TestTemplatePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/template_playbook.yaml",
		configFile:   "default.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "template_playbook should run without error in env: %s, output: %s", envName, output)
			assertFileExists(t, "./test.conf")
			assertFileExists(t, "/tmp/spage/test.conf")
		},
	})
}

func TestShellPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/shell_playbook.yaml",
		configFile:   "default.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "shell_playbook should run without error in env: %s, output: %s", envName, output)
			assertDirectoryExists(t, "/tmp/spage/spage_test")
		},
	})
}

func TestFilePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/file_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "file_playbook should run without error in env: %s, output: %s", envName, output)
			assertFileExists(t, "/tmp/spage/spage_test_file.txt")
		},
	})
}

func TestInvalidPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/invalid_playbook.yaml",
		configFile:   "default.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.NotEqual(t, 0, exitCode, "invalid_playbook should fail in env: %s", envName)
		},
	})
}

func TestRevertPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/revert_playbook.yaml",
		configFile:   "default.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 1, exitCode, "revert_playbook should fail with exit code 1 in env: %s", envName)
			assertFileContains(t, "/tmp/spage/revert_test.txt", "reverted")
		},
	})
}

func TestRevertPlaybookNoRevert(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/revert_playbook.yaml",
		configFile:   "no_revert.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 1, exitCode, "revert_playbook with no_revert should fail with exit code 1 in env: %s", envName)
			assertFileContains(t, "/tmp/spage/revert_test.txt", "not reverted")
		},
	})
}

func TestExecutionModePlaybookParallelFails(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/execution_mode_playbook.yaml",
		configFile:   "default.yaml", // default is parallel
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.NotEqual(t, 0, exitCode, "execution_mode_playbook in parallel should fail in env: %s", envName)
		},
	})
}

func TestExecutionModePlaybookSequentialSucceeds(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/execution_mode_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "execution_mode_playbook in sequential should succeed in env: %s, output: %s", envName, output)
			path := "/tmp/spage/exec_mode_test.txt"
			assertFileContains(t, path, "step3")
			content, err := os.ReadFile(path)
			require.NoError(t, err, "Failed to read file: %s", path)
			assert.Equal(t, 3, strings.Count(string(content), "\n"), "File should have 3 lines")
		},
	})
}

func TestIncludePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/include_playbook.yaml",
		configFile:   "default.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "include_playbook should succeed in env: %s, output: %s", envName, output)
			assertFileExists(t, "/tmp/spage/include_test_main_start.txt")
			assertFileExists(t, "/tmp/spage/include_test_included.txt")
			assertFileExists(t, "/tmp/spage/include_test_main_end.txt")
		},
	})
}

func TestIncludeRolePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/include_role_playbook.yaml",
		configFile:   "default.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "include_role_playbook should succeed in env: %s, output: %s", envName, output)
			assertFileExists(t, "/tmp/spage/include_role_before.txt")
			assertFileExists(t, "/tmp/spage/include_role_task.txt")
			assertFileExists(t, "/tmp/spage/include_role_after.txt")
		},
	})
}

func TestImportTasksPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/import_tasks_playbook.yaml",
		configFile:   "default.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "import_tasks_playbook should succeed in env: %s, output: %s", envName, output)
			assertFileExists(t, "/tmp/spage/import_tasks_before.txt")
			assertFileExists(t, "/tmp/spage/import_tasks_imported.txt")
			assertFileExists(t, "/tmp/spage/import_tasks_after.txt")
		},
	})
}

func TestImportRolePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/import_role_playbook.yaml",
		configFile:   "default.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "import_role_playbook should succeed in env: %s, output: %s", envName, output)
			assertFileExists(t, "/tmp/spage/import_role_before.txt")
			assertFileExists(t, "/tmp/spage/include_role_task.txt") // uses the same task file as include_role
			assertFileExists(t, "/tmp/spage/import_role_after.txt")
		},
	})
}

func TestAssertPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/assert_playbook.yaml",
		configFile:   "default.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.NotEqual(t, 0, exitCode, "assert_playbook should fail in env: %s", envName)
		},
	})
}

func TestRootTasksPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: createTestPlaybook(t, "root_tasks_playbook.yaml", `
---
- name: Root playbook with tasks
  hosts: localhost
  tasks:
    - name: Create a test file with tasks
      shell: echo "Created by root-level tasks section" > /tmp/spage/root_playbook_tasks.txt
`),
		configFile: "default.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "root_tasks_playbook should succeed")
			assertFileContains(t, "/tmp/spage/root_playbook_tasks.txt", "Created by root-level tasks section")
		},
	})
}

func TestRootRolesPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/root_roles_playbook.yaml",
		configFile:   "default.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "root_roles_playbook should succeed")
			assertFileContains(t, "/tmp/spage/root_playbook_role.txt", "Created by root-level roles section")
		},
	})
}

func TestRootBothPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/root_both_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "root_both_playbook should succeed")
			assertFileContains(t, "/tmp/spage/root_playbook_role.txt", "Created by root-level roles section")
			assertFileContains(t, "/tmp/spage/root_playbook_role.txt", "Additional task")
		},
	})
}

func TestStatPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/stat_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "stat_playbook should succeed")
		},
	})
}

func TestCommandPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/command_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.NotEqual(t, 0, exitCode, "command_playbook should fail to test revert")
			assertFileExists(t, "/tmp/spage/spage_command_test_file1.txt")
			assertFileExists(t, "/tmp/spage/spage_command_test_file2.txt")
			assertDirectoryExists(t, "/tmp/spage/spage_command_dir")
			assertFileExists(t, "/tmp/spage/spage_command_test_no_expand.txt")
			assertFileDoesNotExist(t, "/tmp/spage/spage_command_revert.txt")
		},
	})
}

func TestWhenPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/when_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "when_playbook should succeed")
			assertFileExists(t, "/tmp/spage/spage_when_test_should_run.txt")
			assertFileExists(t, "/tmp/spage/spage_when_test_simple_true.txt")
			assertFileDoesNotExist(t, "/tmp/spage/spage_when_test_should_skip.txt")
			assertFileDoesNotExist(t, "/tmp/spage/spage_when_test_nonexistent.txt")
			assertFileDoesNotExist(t, "/tmp/spage/spage_when_test_simple_false.txt")
		},
	})
}

func TestSlurpPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/slurp_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "slurp_playbook should succeed")
		},
	})
}

func TestFailedWhenPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/failed_when_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.NotEqual(t, 0, exitCode, "failed_when_playbook should fail")
			assertFileExists(t, "/tmp/spage/failed_when_succeed.txt")
			assertFileExists(t, "/tmp/spage/failed_when_ignore.txt")
			assertFileExists(t, "/tmp/spage/failed_when_fail.txt")
			assertFileDoesNotExist(t, "/tmp/spage/failed_when_after_actual_fail.txt")
		},
	})
}

func TestLoopSequentialPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/loop_sequential_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "loop_sequential_playbook should succeed")
		},
	})
}

func TestAnsibleBuiltinPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/ansible_builtin_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "ansible_builtin_playbook should succeed")
		},
	})
}

func TestFailModulePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/fail_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.NotEqual(t, 0, exitCode, "fail_module_playbook should fail")
			assertFileContains(t, "/tmp/spage/fail_test.txt", "About to test the fail module")
			assertFileDoesNotContain(t, "/tmp/spage/fail_test.txt", "This message should never appear")
		},
	})
}

func TestLineinfilePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/lineinfile_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "lineinfile_playbook should succeed")
			path := "/tmp/spage/lineinfile_test.txt"
			assertFileContains(t, path, "First line, added with insertbefore BOF.")
			assertFileContains(t, path, "Spage still is awesome, indeed!")
			assertFileContains(t, path, "This is a new line without regexp.")
			assertFileContains(t, path, "Last line, added with insertafter EOF.")
			content, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.NotContains(t, string(content), "Spage was here!")
			assert.NotContains(t, string(content), "# This is a comment to remove")
			assert.NotContains(t, string(content), "This line will be removed by exact match.")

			templatedPath := "/tmp/spage/lineinfile_templated.txt"
			assertFileExists(t, templatedPath)
			assertFileContains(t, templatedPath, "This line comes from a set_fact variable.")
		},
	})
}

func TestLineinfileRevertPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/lineinfile_revert_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.NotEqual(t, 0, exitCode, "lineinfile_revert_playbook should fail")
			assertFileDoesNotExist(t, "/tmp/spage/lineinfile_revert_specific.txt")
		},
	})
}

func TestDelegateToPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/delegate_to_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "delegate_to_playbook should succeed")
			// Assuming local run, all files are on the local machine.
			assertFileExists(t, "/tmp/spage/delegate_test_on_localhost.txt")
			assertFileExists(t, "/tmp/spage/delegate_test_on_inventory_host.txt")
			assertFileExists(t, "/tmp/spage/delegate_test_delegated_to_inventory_host.txt")
		},
	})
}

func TestFactGatheringPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/fact_gathering_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "fact_gathering_playbook should succeed")
		},
	})
}

func TestRunOncePlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/run_once_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "run_once_playbook should succeed")
			// Assuming local run, all files are on the local machine.
			assertFileExists(t, "/tmp/spage/run_once_test.txt")
			assertFileExists(t, "/tmp/spage/normal_task.txt")

			loopFile := "/tmp/spage/run_once_loop.txt"
			assertFileExists(t, loopFile)
			assertFileContains(t, loopFile, "loop_item_first")
			assertFileContains(t, loopFile, "loop_item_second")
			assertFileContains(t, loopFile, "loop_item_third")

			content, err := os.ReadFile(loopFile)
			require.NoError(t, err)
			assert.Equal(t, 3, strings.Count(string(content), "\n"), "run_once loop file should have 3 lines")
		},
	})
}

func TestUntilLoopPlaybook(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/until_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "until_loop_playbook should succeed")
			assertFileExists(t, "/tmp/spage/until_succeeded.txt")
		},
	})
}

func TestTagsConfig(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/tags_playbook.yaml",
		configFile:   "sequential.yaml",
		tags:         "config",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "tags_config should succeed")
			// Tasks that should run with 'config' tag
			assertFileExists(t, "/tmp/spage/tag_test_single_config.txt")
			assertFileExists(t, "/tmp/spage/tag_test_config_deploy.txt")
			assertFileExists(t, "/tmp/spage/tag_test_always.txt") // 'always' tag runs with any tag selection
			// Tasks that should NOT run
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_no_tags.txt")
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_multiple_database_setup.txt")
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_deploy.txt")
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_skip.txt")
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_never.txt")
		},
	})
}

func TestSkipTagsSkip(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/tags_playbook.yaml",
		configFile:   "sequential.yaml",
		skipTags:     "skip",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "skip_tags_skip should succeed")
			// Tasks that should run (all except 'skip' and 'never')
			assertFileExists(t, "/tmp/spage/tag_test_no_tags.txt")
			assertFileExists(t, "/tmp/spage/tag_test_single_config.txt")
			assertFileExists(t, "/tmp/spage/tag_test_multiple_database_setup.txt")
			assertFileExists(t, "/tmp/spage/tag_test_always.txt")
			assertFileExists(t, "/tmp/spage/tag_test_deploy.txt")
			assertFileExists(t, "/tmp/spage/tag_test_config_deploy.txt")
			// Tasks that should NOT run
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_skip.txt")
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_never.txt") // 'never' doesn't run by default
		},
	})
}

func TestTagsNever(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/tags_playbook.yaml",
		configFile:   "sequential.yaml",
		tags:         "never",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "tags_never should succeed")
			// When explicitly selecting 'never' tag, only 'never' and 'always' tasks should run
			assertFileExists(t, "/tmp/spage/tag_test_never.txt")
			assertFileExists(t, "/tmp/spage/tag_test_always.txt")
			// All other tasks should NOT run
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_no_tags.txt")
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_single_config.txt")
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_multiple_database_setup.txt")
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_deploy.txt")
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_config_deploy.txt")
			assertFileDoesNotExist(t, "/tmp/spage/tag_test_skip.txt")
		},
	})
}

func TestPythonFallbackModule(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/python_fallback_module.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "python_fallback_module should succeed")
			// Verify that Python fallback worked by checking the files created
			assertFileExists(t, "/tmp/spage/python_fallback_ping.txt")
			assertFileContains(t, "/tmp/spage/python_fallback_ping.txt", "pong")
		},
	})
}

func TestCheckMode(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/check_mode_playbook.yaml",
		configFile:   "sequential.yaml",
		checkMode:    true,
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "check_mode should succeed")
		},
	})
}

func TestDiffMode(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/diff_mode_playbook.yaml",
		configFile:   "sequential.yaml",
		diffMode:     true,
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "diff_mode should succeed")
			// Verify that all diff operations completed successfully
			assertFileExists(t, "/tmp/spage/diff_mode_no_keyword.txt")
			assertFileContains(t, "/tmp/spage/diff_mode_no_keyword.txt", "no_diff_keyword_completed")
			assertFileExists(t, "/tmp/spage/diff_mode_yes.txt")
			assertFileContains(t, "/tmp/spage/diff_mode_yes.txt", "diff_yes_completed")
			assertFileExists(t, "/tmp/spage/diff_mode_no.txt")
			assertFileContains(t, "/tmp/spage/diff_mode_no.txt", "diff_no_completed")

			// Verify that the lineinfile operations actually worked by checking file contents
			assertFileExists(t, "/tmp/spage/diff_test_file_1.txt")
			assertFileContains(t, "/tmp/spage/diff_test_file_1.txt", "changed content 1")
			assertFileExists(t, "/tmp/spage/diff_test_file_2.txt")
			assertFileContains(t, "/tmp/spage/diff_test_file_2.txt", "changed content 2")
			assertFileExists(t, "/tmp/spage/diff_test_file_3.txt")
			assertFileContains(t, "/tmp/spage/diff_test_file_3.txt", "changed content 3")
		},
	})
}

func TestTemplateDiffMode(t *testing.T) {
	runPlaybookTest(t, playbookTestCase{
		playbookFile: "playbooks/template_diff_test.yaml",
		configFile:   "sequential.yaml",
		diffMode:     true,
		check: func(t *testing.T, envName string, exitCode int, output string) {
			assert.Equal(t, 0, exitCode, "template_diff_mode should succeed")
			// Verify that all template operations completed successfully
			assertFileExists(t, "/tmp/spage/template_diff_no_keyword.txt")
			assertFileContains(t, "/tmp/spage/template_diff_no_keyword.txt", "no_diff_keyword_completed")
			assertFileExists(t, "/tmp/spage/template_diff_yes.txt")
			assertFileContains(t, "/tmp/spage/template_diff_yes.txt", "diff_yes_completed")
			assertFileExists(t, "/tmp/spage/template_diff_no.txt")
			assertFileContains(t, "/tmp/spage/template_diff_no.txt", "diff_no_completed")
			assertFileExists(t, "/tmp/spage/template_diff_different.txt")
			assertFileContains(t, "/tmp/spage/template_diff_different.txt", "different_content_completed")

			// TODO: improve testing
		},
	})
}
