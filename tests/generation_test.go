package tests

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/AlexanderGrooff/spage/cmd"
	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/stretchr/testify/assert"
)

type generationTestCase struct {
	playbookFile string
	configFile   string
	check        func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory)
}

func runGenerationTest(t *testing.T, tc generationTestCase) {
	outputPath := "playbooks/generated_tasks.go"
	// Ensure cleanup runs even on panic/error
	defer func() {
		cleanup(t)
		_ = os.Remove(outputPath)
		if r := recover(); r != nil {
			t.Fatalf("Test panicked: %v", r)
		}
	}()

	configPath := ""
	if tc.configFile != "" {
		configPath = fmt.Sprintf("configs/%s", tc.configFile)
	}
	err := cmd.LoadConfig(configPath)
	assert.NoError(t, err)
	cfg := cmd.GetConfig()
	assert.NotNil(t, cfg)

	graph, err := cmd.GetGraph(tc.playbookFile, []string{}, []string{}, cfg, false)
	assert.NoError(t, err)
	assert.NotNil(t, graph)
	err = graph.SaveToFile(outputPath)
	assert.NoError(t, err)

	vetGeneratedFile(t, outputPath)
	oldCwd, err := pkg.ChangeCWDToPlaybookDir(tc.playbookFile)
	assert.NoError(t, err)
	output, err := exec.Command("go", "run", "generated_tasks.go", "--inventory", "../inventory.yaml", "--limit", "localhost").CombinedOutput()
	_ = os.Chdir(oldCwd)
	fmt.Println(string(output))

	tc.check(t, "generation", 0, err, nil)
}

func vetGeneratedFile(t *testing.T, path string) {
	output, err := exec.Command("go", "vet", path).CombinedOutput()
	fmt.Println(string(output))
	assert.NoError(t, err)
	assert.Empty(t, output)

	output, err = exec.Command("go", "build", path).CombinedOutput()
	fmt.Println(string(output))
	assert.NoError(t, err)
	assert.Empty(t, output)
}

func TestGenerationAnsibleBuiltinPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/ansible_builtin_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "ansible_builtin_playbook should succeed")
			assert.NoError(t, err, "ansible_builtin_playbook should not return an error")
		},
	})
}

func TestGenerationAssertPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/assert_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "assert_playbook generation should succeed")
			assert.Error(t, err, "assert_playbook execution should fail")
		},
	})
}

func TestGenerationBecomeModePlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/become_mode_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "become_mode_playbook should succeed")
			if os.Getuid() == 0 {
				assert.NoError(t, err, "become_mode_playbook should not return an error")
			} else {
				assert.Error(t, err, "become_mode_playbook should return an error")
			}
		},
	})
}

func TestGenerationCleanupPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/cleanup_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "cleanup_playbook should succeed")
			assert.NoError(t, err, "cleanup_playbook should not return an error")
		},
	})
}

func TestGenerationCommandPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/command_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "command_playbook should succeed")
			assert.NoError(t, err, "command_playbook should not return an error")
		},
	})
}

func TestGenerationConnectionLocalPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/connection_local_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "connection_local_playbook should succeed")
			assert.NoError(t, err, "connection_local_playbook should not return an error")
		},
	})
}

func TestGenerationCopyPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/copy_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "copy_playbook should succeed")
			assert.NoError(t, err, "copy_playbook should not return an error")
		},
	})
}

func TestGenerationDebugPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/debug_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "debug_playbook should succeed")
			assert.NoError(t, err, "debug_playbook should not return an error")
		},
	})
}

func TestGenerationDelegateToPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/delegate_to_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "delegate_to_playbook should succeed")
			assert.NoError(t, err, "delegate_to_playbook should not return an error")
		},
	})
}

func TestGenerationDiffModePlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/diff_mode_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "diff_mode_playbook should succeed")
			assert.NoError(t, err, "diff_mode_playbook should not return an error")
		},
	})
}

func TestGenerationExecutionModePlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/execution_mode_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "execution_mode_playbook should succeed")
			assert.NoError(t, err, "execution_mode_playbook should not return an error")
		},
	})
}

func TestGenerationFactGatheringPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/fact_gathering_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "fact_gathering_playbook should succeed")
			assert.NoError(t, err, "fact_gathering_playbook should not return an error")
		},
	})
}

func TestGenerationFailPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/fail_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "fail_playbook should succeed")
			assert.Error(t, err, "fail_playbook should return an error")
		},
	})
}

func TestGenerationFailedWhenPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/failed_when_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "failed_when_playbook should succeed")
			assert.Error(t, err, "failed_when_playbook should return an error")
		},
	})
}

func TestGenerationFilePlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/file_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "file_playbook should succeed")
			assert.NoError(t, err, "file_playbook should not return an error")
		},
	})
}

func TestGenerationGroupVarsPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/group_vars_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "group_vars_playbook should succeed")
			assert.NoError(t, err, "group_vars_playbook should not return an error")
		},
	})
}

func TestGenerationHandlersFailedTasksPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/handlers_failed_tasks_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "handlers_failed_tasks_playbook should succeed")
			assert.Error(t, err, "handlers_failed_tasks_playbook should return an error")
		},
	})
}

func TestGenerationHandlersPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/handlers_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "handlers_playbook should succeed")
			assert.NoError(t, err, "handlers_playbook should not return an error")
		},
	})
}

func TestGenerationHostVarsPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/host_vars_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "host_vars_playbook should succeed")
			assert.NoError(t, err, "host_vars_playbook should not return an error")
		},
	})
}

func TestGenerationImportRolePlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/import_role_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "import_role_playbook should succeed")
			assert.NoError(t, err, "import_role_playbook should not return an error")
		},
	})
}

func TestGenerationImportTasksPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/import_tasks_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "import_tasks_playbook should succeed")
			assert.NoError(t, err, "import_tasks_playbook should not return an error")
		},
	})
}

func TestGenerationImportedTasks(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/imported_tasks.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "imported_tasks should succeed")
			assert.NoError(t, err, "imported_tasks should not return an error")
		},
	})
}

func TestGenerationIncludePlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/include_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "include_playbook should succeed")
			assert.NoError(t, err, "include_playbook should not return an error")
		},
	})
}

func TestGenerationIncludeRolePlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/include_role_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "include_role_playbook should succeed")
			assert.NoError(t, err, "include_role_playbook should not return an error")
		},
	})
}

func TestGenerationIncludedPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/included_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "included_playbook should succeed")
			assert.NoError(t, err, "included_playbook should not return an error")
		},
	})
}

func TestGenerationInvalidPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/invalid_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			// This playbook generation doesn't fail, but it's not valid to run.
			// TODO: Add a test to check that the generated tasks are valid.
			assert.Equal(t, 0, exitCode, "invalid_playbook should succeed")
			assert.Error(t, err, "invalid_playbook should return an error")
		},
	})
}

func TestGenerationJinjaIncludePlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/jinja_include_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "jinja_include_playbook should succeed")
			assert.NoError(t, err, "jinja_include_playbook should not return an error")
		},
	})
}

func TestGenerationJinjaTestsPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/jinja_tests_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "jinja_tests_playbook should succeed")
			assert.NoError(t, err, "jinja_tests_playbook should not return an error")
		},
	})
}

func TestGenerationLineinfilePlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/lineinfile_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "lineinfile_playbook should succeed")
			assert.NoError(t, err, "lineinfile_playbook should not return an error")
		},
	})
}

func TestGenerationLineinfileRevertPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/lineinfile_revert_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "lineinfile_revert_playbook should succeed")
			assert.Error(t, err, "lineinfile_revert_playbook should return an error")
		},
	})
}

func TestGenerationLocalActionPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/local_action_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "local_action_playbook should succeed")
			assert.NoError(t, err, "local_action_playbook should not return an error")
		},
	})
}

func TestGenerationLoopSequentialPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/loop_sequential_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "loop_sequential_playbook should succeed")
			assert.NoError(t, err, "loop_sequential_playbook should not return an error")
		},
	})
}

func TestGenerationMultiStepPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/multi_step_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "multi_step_playbook should succeed")
			assert.NoError(t, err, "multi_step_playbook should not return an error")
		},
	})
}

func TestGenerationOverwritingVariables(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/overwriting_variables.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "overwriting_variables should succeed")
			assert.NoError(t, err, "overwriting_variables should not return an error")
		},
	})
}

func TestGenerationPythonFallbackModule(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/python_fallback_module.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "python_fallback_module should succeed")
			assert.NoError(t, err, "python_fallback_module should not return an error")
		},
	})
}

func TestGenerationRevertPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/revert_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "revert_playbook should succeed")
			assert.Error(t, err, "revert_playbook should return an error")
		},
	})
}

func TestGenerationRootBothPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/root_both_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "root_both_playbook should succeed")
			assert.NoError(t, err, "root_both_playbook should not return an error")
		},
	})
}

func TestGenerationRootRolesPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/root_roles_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "root_roles_playbook should succeed")
			assert.NoError(t, err, "root_roles_playbook should not return an error")
		},
	})
}

func TestGenerationRootTasksPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/root_tasks_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "root_tasks_playbook should succeed")
			assert.NoError(t, err, "root_tasks_playbook should not return an error")
		},
	})
}

func TestGenerationRunOncePlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/run_once_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "run_once_playbook should succeed")
			assert.NoError(t, err, "run_once_playbook should not return an error")
		},
	})
}

func TestGenerationShellPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/shell_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "shell_playbook should succeed")
			assert.NoError(t, err, "shell_playbook should not return an error")
		},
	})
}

func TestGenerationShorthandSyntaxPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/shorthand_syntax_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "shorthand_syntax_playbook should succeed")
			assert.NoError(t, err, "shorthand_syntax_playbook should not return an error")
		},
	})
}

func TestGenerationSlurpPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/slurp_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "slurp_playbook should succeed")
			assert.NoError(t, err, "slurp_playbook should not return an error")
		},
	})
}

func TestGenerationStatPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/stat_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "stat_playbook should succeed")
			assert.NoError(t, err, "stat_playbook should not return an error")
		},
	})
}

func TestGenerationTagsPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/tags_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "tags_playbook should succeed")
			assert.NoError(t, err, "tags_playbook should not return an error")
		},
	})
}

func TestGenerationTemplatePlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/template_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "template_playbook should succeed")
			assert.NoError(t, err, "template_playbook should not return an error")
		},
	})
}

func TestGenerationUntilPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/until_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "until_playbook should succeed")
			assert.NoError(t, err, "until_playbook should not return an error")
		},
	})
}

func TestGenerationVarsPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/vars_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "vars_playbook should succeed")
			assert.NoError(t, err, "vars_playbook should not return an error")
		},
	})
}

func TestGenerationWhenPlaybook(t *testing.T) {
	runGenerationTest(t, generationTestCase{
		playbookFile: "playbooks/when_playbook.yaml",
		configFile:   "sequential.yaml",
		check: func(t *testing.T, envName string, exitCode int, err error, inventory *pkg.Inventory) {
			assert.Equal(t, 0, exitCode, "when_playbook should succeed")
			assert.NoError(t, err, "when_playbook should not return an error")
		},
	})
}
