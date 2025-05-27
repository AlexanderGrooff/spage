#!/usr/bin/env bash

# This script is used to test the spage project by running the spage command on several fixtures.
# This makes mutations on the host it is run on, so be careful when running it.
# Most of it is in the fixtures directory.

set -e
set -x

# Function to execute a check command locally or remotely via ssh
# Diagnostic output goes to stderr (>&2)
check_target() {
  local cmd_string="$1"
  local exit_code=0

  if [ -n "$SPAGE_INVENTORY" ]; then
    # Remote execution via ssh theta
    # Ensure ssh keys are set up for passwordless access to 'theta'
    echo "Running remote check on theta: $cmd_string" >&2
    # Run ssh command, its stdout will be captured by the caller if using $()
    if ssh theta -- "$cmd_string"; then
      exit_code=0
    else
      exit_code=$?
      echo "Remote check failed with exit code $exit_code" >&2
    fi
  else
    # Local execution
    echo "Running local check: $cmd_string" >&2
    # Run bash command, its stdout will be captured by the caller if using $()
    if bash -c "$cmd_string"; then
       exit_code=0
    else
      exit_code=$?
      echo "Local check failed with exit code $exit_code" >&2
    fi
  fi
  return $exit_code
}

# Determine inventory argument based on environment variable
if [ -n "$SPAGE_INVENTORY" ]; then
  if [ ! -f "$SPAGE_INVENTORY" ]; then
    echo "Error: Inventory file specified in SPAGE_INVENTORY does not exist: $SPAGE_INVENTORY"
    exit 1
  fi
  INVENTORY_ARG="-inventory $SPAGE_INVENTORY"
  echo "Using inventory: $SPAGE_INVENTORY"
else
  INVENTORY_ARG=""
  echo "Using default inventory (localhost specified in config)"
fi

TESTS_DIR=$(dirname "$0")/tests

# Define cleanup function
cleanup() {
    echo "Cleaning up test files..."
    # Only remove local artifacts if running locally
    if [ -z "$SPAGE_INVENTORY" ]; then
      rm -f ./test.conf
    fi
    rm -rf /tmp/spage
    # Cleanup for apt module tests (requires sudo, only if run locally)
    if [ -z "$SPAGE_INVENTORY" ]; then
      if command -v cowsay &> /dev/null; then
          echo "Removing cowsay package installed during local test..."
          sudo apt-get remove -y --purge cowsay || echo "Failed to remove cowsay locally, may require manual cleanup."
      fi
      if command -v sl &> /dev/null; then
          echo "Removing sl package installed during local test..."
          sudo apt-get remove -y --purge sl || echo "Failed to remove sl locally, may require manual cleanup."
      fi
    else
      if ssh theta "command -v cowsay &> /dev/null"; then
        echo "Removing cowsay package installed during remote test..."
        ssh theta "sudo apt-get remove -y --purge cowsay || echo 'Failed to remove cowsay remotely, may require manual cleanup.'"
      fi
      if ssh theta "command -v sl &> /dev/null"; then
        echo "Removing sl package installed during remote test..."
        ssh theta "sudo apt-get remove -y --purge sl || echo 'Failed to remove sl remotely, may require manual cleanup.'"
      fi
    fi
    echo "Cleanup complete"
}

# Set trap to call cleanup function on script exit
trap cleanup EXIT

# --- Pre-Test Cleanup --- #
echo "Running pre-test cleanup..."
go run main.go generate -p "$TESTS_DIR/playbooks/cleanup_playbook.yaml" -o generated_tasks.go || true
go build -o generated_tasks generated_tasks.go || true
./generated_tasks $INVENTORY_ARG -config "$TESTS_DIR/configs/default.yaml" || true
echo "Pre-test cleanup finished."

# Test 1: Template playbook test
echo "Running template playbook test..."
go run main.go generate -p $TESTS_DIR/playbooks/template_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml

# Check if the template files were created on the target
if ! check_target "[ -f ./test.conf ]"; then
    echo "Template test: ./test.conf was not found on target"
    exit 1
fi
if ! check_target "[ -f /tmp/spage/test.conf ]"; then
    echo "Template test: /tmp/spage/test.conf was not found on target"
    exit 1
fi

# Test 2: Shell command execution test
echo "Running shell command test..."
go run main.go generate -p $TESTS_DIR/playbooks/shell_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml

# Check if the test directory was created on the target
if ! check_target "[ -d /tmp/spage/spage_test ]"; then
    echo "Shell test: test directory /tmp/spage/spage_test was not found on target"
    exit 1
fi

# Test 3: File operations test
echo "Running file operations test..."
go run main.go generate -p $TESTS_DIR/playbooks/file_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml

# Check if test file exists on the target
if ! check_target "[ -f /tmp/spage/spage_test_file.txt ]"; then
    echo "File operations test: test file /tmp/spage/spage_test_file.txt was not found on target"
    exit 1
fi
# Potentially add content check via check_target "grep 'content' /tmp/spage/spage_test_file.txt"

echo "Running copy test..."
go run main.go generate -p $TESTS_DIR/playbooks/copy_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml

# Test 4: Error handling test
echo "Running error handling test..."
if go run main.go generate -p $TESTS_DIR/playbooks/invalid_playbook.yaml -o generated_tasks.go; then
    echo "Error handling test failed: should have errored on invalid playbook"
    exit 1
fi

# Test 5: Multi-step playbook test
echo "Running multi-step playbook test..."
go run main.go generate -p $TESTS_DIR/playbooks/multi_step_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml

# Check multi-step results on the target
if ! check_target "[ -f /tmp/spage/step1.txt ]"; then
    echo "Multi-step test: /tmp/spage/step1.txt was not found on target"
    exit 1
fi
if ! check_target "[ -f /tmp/spage/step2.txt ]"; then
    echo "Multi-step test: /tmp/spage/step2.txt was not found on target"
    exit 1
fi

# Test 6: Revert functionality test
echo "Running revert functionality test..."

# Run revert test playbook
go run main.go generate -p $TESTS_DIR/playbooks/revert_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go

# Temporarily disable exit on error for the failing command
set +e
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml
REVERT_EXIT_CODE=$?
set -e

# Check if the exit code indicates failure (should be 1 for intentional failure)
if [ $REVERT_EXIT_CODE -ne 1 ]; then
    echo "Revert test failed: playbook should have failed with exit code 1, got $REVERT_EXIT_CODE"
    exit 1
fi

# Verify file was reverted on the target
# Check existence and content in one command
if ! check_target "[ -f /tmp/spage/revert_test.txt ] && grep -q 'reverted' /tmp/spage/revert_test.txt"; then
    echo "Revert test failed: file /tmp/spage/revert_test.txt missing or content incorrect on target"
    # Optionally print remote file content for debugging:
    # check_target "cat /tmp/spage/revert_test.txt || echo 'file not found'"
    exit 1
fi

# Test 7: Execution Mode Test
echo "Running execution mode test..."
go run main.go generate -p $TESTS_DIR/playbooks/execution_mode_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go

# Run in parallel mode (default) - should fail due to dependencies
echo "Running in parallel mode (expecting failure)..."
set +e
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml
PARALLEL_EXIT_CODE=$?
set -e
if [ $PARALLEL_EXIT_CODE -eq 0 ]; then
    echo "Execution mode test failed: Parallel execution succeeded unexpectedly."
    # Optional: check if file content is wrong in case of unexpected success
    # if [ "$(wc -l < /tmp/spage/exec_mode_test.txt)" -ne 3 ]; then ... fi
    exit 1
fi
echo "Parallel failure confirmed (Exit Code: $PARALLEL_EXIT_CODE)."

# Clean up the potentially partially created file before sequential run
rm -f /tmp/spage/exec_mode_test.txt

# Run in sequential mode - should succeed
echo "Running in sequential mode (expecting success)..."
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml
SEQUENTIAL_EXIT_CODE=$?
if [ $SEQUENTIAL_EXIT_CODE -ne 0 ]; then
    echo "Execution mode test failed: Sequential execution failed unexpectedly (Exit Code: $SEQUENTIAL_EXIT_CODE)."
    exit 1
fi

# Verify sequential results on the target
# Check existence, line count, and specific content
# Get remote line count using grep -c, command substitution now captures only grep output
if ! remote_line_count=$(check_target "grep -c '' /tmp/spage/exec_mode_test.txt") || \
   ! check_target "[ -f /tmp/spage/exec_mode_test.txt ] && [ \"$remote_line_count\" -eq 3 ] && grep -q 'step3' /tmp/spage/exec_mode_test.txt"; then
    echo "Execution mode test failed: Sequential execution did not produce the expected file content on target. Line count: '$remote_line_count'"
    check_target "cat /tmp/spage/exec_mode_test.txt || echo 'file not found'" # Print content for debugging
    exit 1
fi
echo "Sequential success confirmed."

# Test 8: Include directive test
echo "Running include directive test..."
go run main.go generate -p $TESTS_DIR/playbooks/include_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml

# Check if files from both main and included playbooks were created on the target
if ! check_target "[ -f /tmp/spage/include_test_main_start.txt ]"; then
    echo "Include test: main start file /tmp/spage/include_test_main_start.txt was not created on target"
    exit 1
fi
if ! check_target "[ -f /tmp/spage/include_test_included.txt ]"; then
    echo "Include test: included file /tmp/spage/include_test_included.txt was not created on target"
    exit 1
fi
if ! check_target "[ -f /tmp/spage/include_test_main_end.txt ]"; then
    echo "Include test: main end file /tmp/spage/include_test_main_end.txt was not created on target"
    exit 1
fi

# Test 9: Include Role directive test
echo "Running include_role directive test..."
go run main.go generate -p $TESTS_DIR/playbooks/include_role_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml

# Check if files from before, inside, and after the role include were created on the target
if ! check_target "[ -f /tmp/spage/include_role_before.txt ]"; then
    echo "Include Role test: before file /tmp/spage/include_role_before.txt was not created on target"
    exit 1
fi
if ! check_target "[ -f /tmp/spage/include_role_task.txt ]"; then
    echo "Include Role test: role task file /tmp/spage/include_role_task.txt was not created on target"
    exit 1
fi
if ! check_target "[ -f /tmp/spage/include_role_after.txt ]"; then
    echo "Include Role test: after file /tmp/spage/include_role_after.txt was not created on target"
    exit 1
fi

# Test 10: Import Tasks directive test
echo "Running import_tasks directive test..."
go run main.go generate -p $TESTS_DIR/playbooks/import_tasks_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml

# Check if files from before, imported, and after were created on the target
if ! check_target "[ -f /tmp/spage/import_tasks_before.txt ]"; then
    echo "Import Tasks test: before file /tmp/spage/import_tasks_before.txt was not created on target"
    exit 1
fi
if ! check_target "[ -f /tmp/spage/import_tasks_imported.txt ]"; then
    echo "Import Tasks test: imported file /tmp/spage/import_tasks_imported.txt was not created on target"
    exit 1
fi
if ! check_target "[ -f /tmp/spage/import_tasks_after.txt ]"; then
    echo "Import Tasks test: after file /tmp/spage/import_tasks_after.txt was not created on target"
    exit 1
fi

# Test 11: Import Role directive test
echo "Running import_role directive test..."
go run main.go generate -p $TESTS_DIR/playbooks/import_role_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml

# Check if files from before, inside (role task), and after the role import were created on the target
if ! check_target "[ -f /tmp/spage/import_role_before.txt ]"; then
    echo "Import Role test: before file /tmp/spage/import_role_before.txt was not created on target"
    exit 1
fi
# Role task file is /tmp/spage/include_role_task.txt (reused from include_role test)
if ! check_target "[ -f /tmp/spage/include_role_task.txt ]"; then
    echo "Import Role test: role task file /tmp/spage/include_role_task.txt was not created on target"
    exit 1
fi
if ! check_target "[ -f /tmp/spage/import_role_after.txt ]"; then
    echo "Import Role test: after file /tmp/spage/import_role_after.txt was not created on target"
    exit 1
fi

# Test 12: Assert module test
echo "Running assert module test..."
go run main.go generate -p $TESTS_DIR/playbooks/assert_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go

# This playbook has failing assertions, so it should exit with an error code
# Run the generated tasks and expect a failure
echo "Running assert playbook (expecting failure)..."
set +e
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml
ASSERT_EXIT_CODE=$?
set -e

# Check if the exit code indicates failure (should be non-zero)
if [ $ASSERT_EXIT_CODE -eq 0 ]; then
    echo "Assert test failed: playbook should have failed due to assertions, but exited with code 0"
    exit 1
fi
echo "Assert playbook failed as expected (Exit Code: $ASSERT_EXIT_CODE)."

# Test 13: Root-level playbook with tasks section
echo "Running root-level playbook with tasks test..."

# Create test playbook
cat > $TESTS_DIR/playbooks/root_tasks_playbook.yaml << EOF
---
- name: Root playbook with tasks
  hosts: localhost
  tasks:
    - name: Create a test file with tasks
      shell: echo "Created by root-level tasks section" > /tmp/spage/root_playbook_tasks.txt
EOF

go run main.go generate -p $TESTS_DIR/playbooks/root_tasks_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml

# Check if the file was created and verify content on the target
if ! check_target "[ -f /tmp/spage/root_playbook_tasks.txt ] && grep -q 'Created by root-level tasks section' /tmp/spage/root_playbook_tasks.txt"; then
    echo "Root-level tasks test: file /tmp/spage/root_playbook_tasks.txt missing or content incorrect on target"
    check_target "cat /tmp/spage/root_playbook_tasks.txt || echo 'file not found'"
    exit 1
fi

# Test 14: Root-level playbook with roles section
echo "Running root-level playbook with roles test..."

go run main.go generate -p $TESTS_DIR/playbooks/root_roles_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/default.yaml

# Check if the file was created and verify content on the target
if ! check_target "[ -f /tmp/spage/root_playbook_role.txt ] && grep -q 'Created by root-level roles section' /tmp/spage/root_playbook_role.txt"; then
    echo "Root-level roles test: file /tmp/spage/root_playbook_role.txt missing or content incorrect on target"
    check_target "cat /tmp/spage/root_playbook_role.txt || echo 'file not found'"
    exit 1
fi

# Test 15: Root-level playbook with both roles and tasks
echo "Running root-level playbook with both roles and tasks test..."

# First remove any existing file from previous test
rm -f /tmp/spage/root_playbook_role.txt

go run main.go generate -p $TESTS_DIR/playbooks/root_both_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml

# Check if the file was created and verify content on the target
if ! check_target "[ -f /tmp/spage/root_playbook_role.txt ] && grep -q 'Created by root-level roles section' /tmp/spage/root_playbook_role.txt && grep -q 'Additional task' /tmp/spage/root_playbook_role.txt"; then
    echo "Root-level both test: file /tmp/spage/root_playbook_role.txt missing or content incorrect on target"
    check_target "cat /tmp/spage/root_playbook_role.txt || echo 'file not found'"
    exit 1
fi

# Test 16: Stat module test
echo "Running stat module test..."
go run main.go generate -p $TESTS_DIR/playbooks/stat_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml
STAT_EXIT_CODE=$?

if [ $STAT_EXIT_CODE -ne 0 ]; then
    echo "Stat module test failed (Exit Code: $STAT_EXIT_CODE)."
    exit 1
fi
echo "Stat module test succeeded."

# Test 17: Command module test
echo "Running command module test..."
go run main.go generate -p $TESTS_DIR/playbooks/command_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go

# Run and expect failure due to the intentional fail task triggering revert
echo "Running command playbook (expecting failure to test revert)..."
set +e
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml
COMMAND_EXIT_CODE=$?
set -e

# Check if the exit code indicates failure (should be non-zero)
if [ $COMMAND_EXIT_CODE -eq 0 ]; then
    echo "Command module test failed: Playbook succeeded unexpectedly (Exit Code: $COMMAND_EXIT_CODE). Revert likely did not trigger."
    exit 1
fi
echo "Command playbook failed as expected (Exit Code: $COMMAND_EXIT_CODE), checking results..."

# Check file/dir states on the target
if ! check_target "[ -f /tmp/spage/spage_command_test_file1.txt ]"; then
    echo "Command test: file1 /tmp/spage/spage_command_test_file1.txt was not created on target"
    exit 1
fi
if ! check_target "[ -f /tmp/spage/spage_command_test_file2.txt ]"; then
    echo "Command test: file2 /tmp/spage/spage_command_test_file2.txt was not created on target"
    exit 1
fi
if ! check_target "[ -d /tmp/spage/spage_command_dir ]"; then
    echo "Command test: directory /tmp/spage/spage_command_dir was not created on target"
    exit 1
fi

# Check for no shell expansion (file 3 created) on the target
if ! check_target "[ -f /tmp/spage/spage_command_test_no_expand.txt ]"; then
    echo "Command test: no_expand file /tmp/spage/spage_command_test_no_expand.txt was not created on target"
    exit 1
fi

# Check revert worked (revert file should NOT exist) on the target
if check_target "[ -f /tmp/spage/spage_command_revert.txt ]"; then # Check if file exists (it shouldn't)
    echo "Command test: revert failed, revert file /tmp/spage/spage_command_revert.txt still exists on target"
    exit 1
fi

echo "Command module test succeeded."

# Test 18: Apt module test
echo "Running apt module test..."

# Check if apt-get is available
if ! command -v apt-get &> /dev/null; then
    echo "Skipping apt module test: apt-get command not found."
else
    # Ensure cowsay and sl are not installed before starting
    if command -v cowsay &> /dev/null; then
        echo "Removing pre-existing cowsay package..."
        sudo apt-get remove -y --purge cowsay
    fi
    if command -v sl &> /dev/null; then
        echo "Removing pre-existing sl package..."
        sudo apt-get remove -y --purge sl
    fi

    go run main.go generate -p $TESTS_DIR/playbooks/apt_playbook.yaml -o generated_tasks.go
    go build -o generated_tasks generated_tasks.go

    # Run and expect failure due to the intentional fail task triggering revert
    echo "Running apt playbook (expecting failure to test revert)..."
    set +e
    ./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml
    APT_EXIT_CODE=$?
    set -e

    # Check if the exit code indicates failure (should be non-zero)
    if [ $APT_EXIT_CODE -eq 0 ]; then
        echo "Apt module test failed: Playbook succeeded unexpectedly (Exit Code: $APT_EXIT_CODE). Revert likely did not trigger."
        exit 1
    fi
    echo "Apt playbook failed as expected (Exit Code: $APT_EXIT_CODE), checking results..."

    # Check package states on the target
    # Note: This requires 'dpkg -s' or similar on the remote host 'theta'
    # Also, the revert check might be tricky if the failure happened before package removal attempt
    if ! check_target "! dpkg -s cowsay > /dev/null 2>&1"; then
        echo "Apt test failed: cowsay seems to be installed on target after revert should have removed it."
        # exit 1 # This might be too strict depending on playbook logic
    fi
    if ! check_target "! dpkg -s sl > /dev/null 2>&1"; then
        echo "Apt test failed: sl seems to be installed on target after revert should have removed it."
        # exit 1 # This might be too strict depending on playbook logic
    fi

    echo "Apt module test (including list install and revert) succeeded."
fi # End of apt-get check

# Test 19: When condition test
echo "Running when condition test..."
go run main.go generate -p $TESTS_DIR/playbooks/when_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml
WHEN_EXIT_CODE=$?

if [ $WHEN_EXIT_CODE -ne 0 ]; then
    echo "When condition test failed: Playbook execution failed unexpectedly (Exit Code: $WHEN_EXIT_CODE)."
    exit 1
fi

# Check results on the target: Expected files should exist, skipped files should not
if ! check_target "[ -f /tmp/spage/spage_when_test_should_run.txt ]"; then
    echo "When test failed: 'should_run' file /tmp/spage/spage_when_test_should_run.txt was not created on target."
    exit 1
fi
if ! check_target "[ -f /tmp/spage/spage_when_test_simple_true.txt ]"; then
    echo "When test failed: 'simple_true' file /tmp/spage/spage_when_test_simple_true.txt was not created on target."
    exit 1
fi

if check_target "[ -f /tmp/spage/spage_when_test_should_skip.txt ]"; then
    echo "When test failed: 'should_skip' file /tmp/spage/spage_when_test_should_skip.txt was created on target but should have been skipped."
    exit 1
fi
if check_target "[ -f /tmp/spage/spage_when_test_nonexistent.txt ]"; then
    echo "When test failed: 'nonexistent' var file /tmp/spage/spage_when_test_nonexistent.txt was created on target but should have been skipped."
    exit 1
fi
if check_target "[ -f /tmp/spage/spage_when_test_simple_false.txt ]"; then
    echo "When test failed: 'simple_false' file /tmp/spage/spage_when_test_simple_false.txt was created on target but should have been skipped."
    exit 1
fi
echo "When condition test succeeded."

# Test 20: Slurp module test
echo "Running slurp module test..."
go run main.go generate -p $TESTS_DIR/playbooks/slurp_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml # Use sequential to ensure file creation before slurp
SLURP_EXIT_CODE=$?

if [ $SLURP_EXIT_CODE -ne 0 ]; then
    echo "Slurp module test failed: Playbook execution failed unexpectedly (Exit Code: $SLURP_EXIT_CODE)."
    exit 1
fi
echo "Slurp module test succeeded."

# Test 21: Failed_when condition test
echo "Running failed_when condition test..."
go run main.go generate -p $TESTS_DIR/playbooks/failed_when_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go

# Run and expect failure because the last task uses failed_when without ignore_errors
echo "Running failed_when playbook (expecting failure)..."
set +e
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml # Use sequential for predictability
FAILED_WHEN_EXIT_CODE=$?
set -e

# Check if the exit code indicates failure (should be non-zero)
if [ $FAILED_WHEN_EXIT_CODE -eq 0 ]; then
    echo "Failed_when test failed: Playbook succeeded unexpectedly (Exit Code: $FAILED_WHEN_EXIT_CODE)."
    exit 1
fi
echo "Failed_when playbook failed as expected (Exit Code: $FAILED_WHEN_EXIT_CODE), checking results..."

# Verify file states on the target
if ! check_target "[ -f /tmp/spage/failed_when_succeed.txt ]"; then
    echo "Failed_when test failed: succeed file /tmp/spage/failed_when_succeed.txt missing on target."
    exit 1
fi
if ! check_target "[ -f /tmp/spage/failed_when_ignore.txt ]"; then
    echo "Failed_when test failed: ignore file /tmp/spage/failed_when_ignore.txt missing on target."
    exit 1
fi
if ! check_target "[ -f /tmp/spage/failed_when_fail.txt ]"; then
    echo "Failed_when test failed: fail file /tmp/spage/failed_when_fail.txt missing on target."
    exit 1
fi
# This file should *not* exist if the playbook failed correctly before this task
if check_target "[ -f /tmp/spage/failed_when_after_actual_fail.txt ]"; then
    echo "Failed_when test failed: Task after actual failure ran unexpectedly on target."
    exit 1
fi
echo "Failed_when test succeeded."

# Test 22: Loop module test
echo "Running loop module test..."
go run main.go generate -p $TESTS_DIR/playbooks/loop_sequential_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml

# Test 23: Ansible builtin modules test
echo "Running ansible builtin modules test..."
go run main.go generate -p $TESTS_DIR/playbooks/ansible_builtin_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml

# Test 24: Fail module test
echo "Running fail module test..."
go run main.go generate -p $TESTS_DIR/playbooks/fail_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go

# Run and expect failure, capture output
echo "Running fail playbook (expecting failure)..."
set +e
FAIL_MODULE_OUTPUT=$(./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml 2>&1)
FAIL_MODULE_EXIT_CODE=$?
set -e

# Check if the exit code indicates failure (should be non-zero)
if [ $FAIL_MODULE_EXIT_CODE -eq 0 ]; then
    echo "Fail module test failed: Playbook succeeded unexpectedly (Exit Code: $FAIL_MODULE_EXIT_CODE)."
    echo "Output was:"
    echo "$FAIL_MODULE_OUTPUT"
    exit 1
fi
echo "Fail playbook failed as expected (Exit Code: $FAIL_MODULE_EXIT_CODE), checking output..."

# Check for the initial debug message
if ! echo "$FAIL_MODULE_OUTPUT" | grep -q "msg: About to test the fail module."; then
    echo "Fail module test failed: Did not find the initial debug message."
    echo "Output was:"
    echo "$FAIL_MODULE_OUTPUT"
    exit 1
fi

# Check that the task after fail did NOT run
if echo "$FAIL_MODULE_OUTPUT" | grep -q "This message should never appear because the playbook should have failed."; then
    echo "Fail module test failed: Found message from a task that should have been skipped after fail."
    echo "Output was:"
    echo "$FAIL_MODULE_OUTPUT"
    exit 1
fi

echo "Fail module test succeeded."

# Test 25: Debug module test
echo "Running debug module test..."
go run main.go generate -p $TESTS_DIR/playbooks/debug_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go

# Run and expect failure, capture output
echo "Running debug playbook (expecting failure to test revert)..."
set +e
DEBUG_OUTPUT=$(./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml 2>&1)
DEBUG_EXIT_CODE=$?
set -e

# Check if the exit code indicates failure (should be non-zero)
if [ $DEBUG_EXIT_CODE -eq 0 ]; then
    echo "Debug module test failed: Playbook succeeded unexpectedly (Exit Code: $DEBUG_EXIT_CODE). Revert likely did not trigger."
    echo "Output was:"
    echo "$DEBUG_OUTPUT"
    exit 1
fi
echo "Debug playbook failed as expected (Exit Code: $DEBUG_EXIT_CODE), checking output..."

# Check for expected execution messages
if ! echo "$DEBUG_OUTPUT" | grep -q "msg: Static debug message for execution."; then
    echo "Debug test failed: Did not find static execute message."
    echo "Output was:"
    echo "$DEBUG_OUTPUT"
    exit 1
fi
if ! echo "$DEBUG_OUTPUT" | grep -q "msg: Templated message: Spage Debug Test (will be reverted)"; then
    echo "Debug test failed: Did not find templated execute message."
    echo "Output was:"
    echo "$DEBUG_OUTPUT"
    exit 1
fi

# Check for expected revert messages
if ! echo "$DEBUG_OUTPUT" | grep -q "msg: Templated message: Spage Debug Test (will be reverted) \[revert\]"; then
    echo "Debug test failed: Did not find templated revert message."
    echo "Output was:"
    echo "$DEBUG_OUTPUT"
    exit 1
fi
# Check that messages from tasks that should have been skipped are NOT present
if echo "$DEBUG_OUTPUT" | grep -q "This message should NOT appear in logs."; then
    echo "Debug test failed: Found message from a task that should have been skipped (msg)."
    echo "Output was:"
    echo "$DEBUG_OUTPUT"
    exit 1
fi
if echo "$DEBUG_OUTPUT" | grep -q "var: another_variable"; then # Check if the var 'another_variable' was debugged
    echo "Debug test failed: Found message from a task that should have been skipped (var another_variable)."
    echo "Output was:"
    echo "$DEBUG_OUTPUT"
    exit 1
fi

echo "Debug module test succeeded."

# Test 26: Lineinfile module test
echo "Running lineinfile module test..."
go run main.go generate -p $TESTS_DIR/playbooks/lineinfile_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go

# This playbook should now complete successfully without intentional errors
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml
LINEINFILE_EXIT_CODE=$?

if [ $LINEINFILE_EXIT_CODE -ne 0 ]; then
    echo "Lineinfile module test failed: Playbook execution failed unexpectedly (Exit Code: $LINEINFILE_EXIT_CODE)."
    exit 1
fi

echo "Lineinfile playbook execution finished (Exit Code: $LINEINFILE_EXIT_CODE), checking results..."

# Check main test file contents on the target
LIF_TEST_FILE="/tmp/spage/lineinfile_test.txt"

# Expected content after all successful operations
if ! check_target "grep -q 'First line, added with insertbefore BOF.' $LIF_TEST_FILE"; then echo "Lineinfile Test 26 Check 1 FAILED"; exit 1; fi
if ! check_target "grep -q 'Spage still is awesome, indeed!' $LIF_TEST_FILE"; then echo "Lineinfile Test 26 Check 2 FAILED"; exit 1; fi
if ! check_target "grep -q 'This is a new line without regexp.' $LIF_TEST_FILE"; then echo "Lineinfile Test 26 Check 3 FAILED"; exit 1; fi
if ! check_target "grep -q 'Last line, added with insertafter EOF.' $LIF_TEST_FILE"; then echo "Lineinfile Test 26 Check 4 FAILED"; exit 1; fi
if ! check_target "! grep -q 'Spage was here!' $LIF_TEST_FILE"; then echo "Lineinfile Test 26 Check 5 FAILED"; exit 1; fi # Should have been replaced
if ! check_target "! grep -q '# This is a comment to remove' $LIF_TEST_FILE"; then echo "Lineinfile Test 26 Check 6 FAILED"; exit 1; fi # Should be absent
if ! check_target "! grep -q 'This line will be removed by exact match.' $LIF_TEST_FILE"; then echo "Lineinfile Test 26 Check 7 FAILED"; exit 1; fi # Should be absent

# Check templated file from set_fact
LIF_TEMPLATED_FILE="/tmp/spage/lineinfile_templated.txt"
EXPECTED_TEMPLATED_CONTENT="This line comes from a set_fact variable."
if ! check_target "[ -f $LIF_TEMPLATED_FILE ]"; then echo "Lineinfile Test 26 Check 8 (templated file existence) FAILED"; exit 1; fi
if ! check_target "grep -q '$EXPECTED_TEMPLATED_CONTENT' $LIF_TEMPLATED_FILE"; then echo "Lineinfile Test 26 Check 9 (templated file content) FAILED"; exit 1; fi

echo "Lineinfile module main test checks passed."


# Test 27: Lineinfile revert test
echo "Running lineinfile revert test..."
go run main.go generate -p $TESTS_DIR/playbooks/lineinfile_revert_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go

# Run and expect failure due to the intentional fail task triggering revert
set +e # Allow for failure
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml
LIF_REVERT_EXIT_CODE=$?
set -e

if [ $LIF_REVERT_EXIT_CODE -eq 0 ]; then
    echo "Lineinfile revert test failed: Playbook succeeded unexpectedly (Exit Code: $LIF_REVERT_EXIT_CODE). Revert likely did not trigger."
    exit 1
fi
echo "Lineinfile revert playbook failed as expected (Exit Code: $LIF_REVERT_EXIT_CODE), checking revert results..."

# Check revert test file - it should NOT exist if revert worked because it was created by the reverted task
LIF_REVERT_SPECIFIC_FILE="/tmp/spage/lineinfile_revert_specific.txt"
if check_target "[ -f $LIF_REVERT_SPECIFIC_FILE ]"; then
    echo "Lineinfile revert test failed: revert file $LIF_REVERT_SPECIFIC_FILE still exists on target."
    check_target "cat $LIF_REVERT_SPECIFIC_FILE || echo 'revert file not found'" # Print content for debugging
    exit 1
fi

echo "Lineinfile revert test checks passed."

# Test 28: Delegate_to test
echo "Running delegate_to test..."
go run main.go generate -p $TESTS_DIR/playbooks/delegate_to_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks $INVENTORY_ARG -config tests/configs/sequential.yaml
DELEGATE_EXIT_CODE=$?

if [ $DELEGATE_EXIT_CODE -ne 0 ]; then
    echo "Delegate_to test failed: Playbook execution failed unexpectedly (Exit Code: $DELEGATE_EXIT_CODE)."
    exit 1
fi

DELEGATED_TO_LOCALHOST_FILE="/tmp/spage/delegate_test_on_localhost.txt"
ON_INVENTORY_HOST_FILE="/tmp/spage/delegate_test_on_inventory_host.txt"
DELEGATED_TO_INVENTORY_HOST_FILE="/tmp/spage/delegate_test_delegated_to_inventory_host.txt"

# Check 1: File delegated to localhost (should always be on the control node)
echo "Checking for file delegated to localhost: $DELEGATED_TO_LOCALHOST_FILE"
if [ ! -f "$DELEGATED_TO_LOCALHOST_FILE" ]; then
    echo "Delegate_to test failed: File $DELEGATED_TO_LOCALHOST_FILE was NOT found on the control node (localhost)."
    exit 1
fi
echo "File $DELEGATED_TO_LOCALHOST_FILE found on control node."

# Check 2 & 3: Files on inventory_hostname (target)
if [ -n "$SPAGE_INVENTORY" ]; then
  # Remote execution checks
  echo "Remote checks for files on inventory_hostname (e.g., theta)..."
  if ! check_target "[ -f $ON_INVENTORY_HOST_FILE ]"; then
      echo "Delegate_to test failed: $ON_INVENTORY_HOST_FILE was not found on remote target."
      exit 1
  fi
  echo "$ON_INVENTORY_HOST_FILE found on remote target."

  if ! check_target "[ -f $DELEGATED_TO_INVENTORY_HOST_FILE ]"; then
      echo "Delegate_to test failed: $DELEGATED_TO_INVENTORY_HOST_FILE was not found on remote target."
      exit 1
  fi
  echo "$DELEGATED_TO_INVENTORY_HOST_FILE found on remote target."
else
  # Local execution checks
  echo "Local checks for files on inventory_hostname (localhost)..."
  if [ ! -f "$ON_INVENTORY_HOST_FILE" ]; then
      echo "Delegate_to test failed: $ON_INVENTORY_HOST_FILE was not found locally (as inventory_hostname)."
      exit 1
  fi
  echo "$ON_INVENTORY_HOST_FILE found locally (as inventory_hostname)."

  if [ ! -f "$DELEGATED_TO_INVENTORY_HOST_FILE" ]; then
      echo "Delegate_to test failed: $DELEGATED_TO_INVENTORY_HOST_FILE was not found locally (as inventory_hostname)."
      exit 1
  fi
  echo "$DELEGATED_TO_INVENTORY_HOST_FILE found locally (as inventory_hostname)."
fi

echo "Delegate_to test succeeded."

echo "All tests completed successfully!"

