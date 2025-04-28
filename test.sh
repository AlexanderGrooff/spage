#!/usr/bin/env bash

# This script is used to test the spage project by running the spage command on several fixtures.
# This makes mutations on the host it is run on, so be careful when running it.
# Most of it is in the fixtures directory.

set -e
set -x

TESTS_DIR=$(dirname "$0")/tests

# Define cleanup function
cleanup() {
    echo "Cleaning up test files..."
    rm -f ./test.conf
    rm -f /tmp/test.conf
    rm -f /tmp/spage_test_file.txt
    rm -f /tmp/spage_test_file2.txt
    rm -rf /tmp/spage_test
    rm -f /tmp/step1.txt
    rm -f /tmp/step2.txt
    rm -f /tmp/revert_test.txt
    rm -f /tmp/revert_test.conf
    rm -rf /tmp/revert_test_dir
    rm -f generated_tasks
    rm -f /tmp/exec_mode_test.txt
    rm -f /tmp/include_test_main_start.txt
    rm -f /tmp/include_test_included.txt
    rm -f /tmp/include_test_main_end.txt
    rm -f /tmp/include_role_before.txt
    rm -f /tmp/include_role_task.txt
    rm -f /tmp/include_role_after.txt
    rm -f /tmp/import_tasks_before.txt
    rm -f /tmp/import_tasks_imported.txt
    rm -f /tmp/import_tasks_after.txt
    rm -f /tmp/import_role_before.txt
    rm -f /tmp/import_role_after.txt
    rm -f /tmp/root_playbook_tasks.txt
    rm -f /tmp/root_playbook_role.txt
    rm -f /tmp/spage_stat_test_file.txt /tmp/spage_stat_test_link.txt
    rm -f /tmp/spage_file_test_dest.txt
    rm -rf /tmp/spage_file_test_name.txt
    rm -f /tmp/spage_file_test_link_dest.txt
    # Cleanup for command module tests
    rm -f /tmp/spage_command_test_file1.txt
    rm -f /tmp/spage_command_test_file2.txt
    rm -f /tmp/spage_command_test_no_expand.txt
    rm -rf /tmp/spage_command_dir
    rm -f /tmp/spage_command_revert.txt
    # Cleanup for apt module tests (requires sudo)
    if command -v cowsay &> /dev/null; then
        echo "Removing cowsay package installed during test..."
        sudo apt-get remove -y --purge cowsay || echo "Failed to remove cowsay, may require manual cleanup."
    fi
    if command -v sl &> /dev/null; then
        echo "Removing sl package installed during test..."
        sudo apt-get remove -y --purge sl || echo "Failed to remove sl, may require manual cleanup."
    fi
    # Cleanup for when module tests
    rm -f /tmp/spage_when_test_should_run.txt
    rm -f /tmp/spage_when_test_should_skip.txt
    rm -f /tmp/spage_when_test_nonexistent.txt
    rm -f /tmp/spage_when_test_simple_true.txt
    rm -f /tmp/spage_when_test_simple_false.txt
    # Cleanup for slurp module test
    rm -f /tmp/spage_slurp_test.txt
    echo "Cleanup complete"
}

# Set trap to call cleanup function on script exit
trap cleanup EXIT

# Test 1: Template playbook test
echo "Running template playbook test..."
go run main.go generate -p $TESTS_DIR/playbooks/template_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/default.yaml

# Check if the template files were created
if [ ! -f ./test.conf ]; then
    echo "Template test: test.conf was not created"
    exit 1
fi
if [ ! -f /tmp/test.conf ]; then
    echo "Template test: /tmp/test.conf was not created"
    exit 1
fi

# Test 2: Shell command execution test
echo "Running shell command test..."
go run main.go generate -p $TESTS_DIR/playbooks/shell_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/default.yaml

# Check if the test directory was created
if [ ! -d /tmp/spage_test ]; then
    echo "Shell test: test directory was not created"
    exit 1
fi

# Test 3: File operations test
echo "Running file operations test..."
go run main.go generate -p $TESTS_DIR/playbooks/file_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/sequential.yaml

# Check if test file exists with correct content
if [ ! -f /tmp/spage_test_file.txt ]; then
    echo "File operations test: test file was not created"
    exit 1
fi

echo "Running copy test..."
go run main.go generate -p $TESTS_DIR/playbooks/copy_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/default.yaml

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
./generated_tasks -config tests/configs/default.yaml

# Check multi-step results
if [ ! -f /tmp/step1.txt ] || [ ! -f /tmp/step2.txt ]; then
    echo "Multi-step test: not all step files were created"
    exit 1
fi

# Test 6: Revert functionality test
echo "Running revert functionality test..."

# Create initial state
echo "initial content" > /tmp/revert_test.txt
chmod 644 /tmp/revert_test.txt
mkdir -p /tmp/revert_test_dir
echo "initial template" > /tmp/revert_test.conf

# Run revert test playbook
go run main.go generate -p $TESTS_DIR/playbooks/revert_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go

# Temporarily disable exit on error for the failing command
set +e
./generated_tasks -config tests/configs/default.yaml
REVERT_EXIT_CODE=$?
set -e

# Check if the exit code indicates failure (should be 1 for intentional failure)
if [ $REVERT_EXIT_CODE -ne 1 ]; then
    echo "Revert test failed: playbook should have failed with exit code 1, got $REVERT_EXIT_CODE"
    exit 1
fi

# Verify files were reverted to original state
if [ ! -f /tmp/revert_test.txt ] || [ "$(cat /tmp/revert_test.txt)" != "reverted" ]; then
    echo "Revert test failed: file content was not reverted"
    exit 1
fi

# Test 7: Execution Mode Test
echo "Running execution mode test..."
go run main.go generate -p $TESTS_DIR/playbooks/execution_mode_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go

# Run in parallel mode (default) - should fail due to dependencies
echo "Running in parallel mode (expecting failure)..."
set +e
./generated_tasks -config tests/configs/default.yaml
PARALLEL_EXIT_CODE=$?
set -e
if [ $PARALLEL_EXIT_CODE -eq 0 ]; then
    echo "Execution mode test failed: Parallel execution succeeded unexpectedly."
    # Optional: check if file content is wrong in case of unexpected success
    # if [ "$(wc -l < /tmp/exec_mode_test.txt)" -ne 3 ]; then ... fi
    exit 1
fi
echo "Parallel failure confirmed (Exit Code: $PARALLEL_EXIT_CODE)."

# Clean up the potentially partially created file before sequential run
rm -f /tmp/exec_mode_test.txt

# Run in sequential mode - should succeed
echo "Running in sequential mode (expecting success)..."
./generated_tasks -config tests/configs/sequential.yaml
SEQUENTIAL_EXIT_CODE=$?
if [ $SEQUENTIAL_EXIT_CODE -ne 0 ]; then
    echo "Execution mode test failed: Sequential execution failed unexpectedly (Exit Code: $SEQUENTIAL_EXIT_CODE)."
    exit 1
fi

# Verify sequential results
if [ ! -f /tmp/exec_mode_test.txt ] || [ "$(wc -l < /tmp/exec_mode_test.txt)" -ne 3 ] || ! grep -q "step3" /tmp/exec_mode_test.txt; then
    echo "Execution mode test failed: Sequential execution did not produce the expected file content."
    cat /tmp/exec_mode_test.txt # Print content for debugging
    exit 1
fi
echo "Sequential success confirmed."

# Test 8: Include directive test
echo "Running include directive test..."
go run main.go generate -p $TESTS_DIR/playbooks/include_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/default.yaml

# Check if files from both main and included playbooks were created
if [ ! -f /tmp/include_test_main_start.txt ]; then
    echo "Include test: main start file was not created"
    exit 1
fi
if [ ! -f /tmp/include_test_included.txt ]; then
    echo "Include test: included file was not created"
    exit 1
fi
if [ ! -f /tmp/include_test_main_end.txt ]; then
    echo "Include test: main end file was not created"
    exit 1
fi

# Test 9: Include Role directive test
echo "Running include_role directive test..."
go run main.go generate -p $TESTS_DIR/playbooks/include_role_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/default.yaml

# Check if files from before, inside, and after the role include were created
if [ ! -f /tmp/include_role_before.txt ]; then
    echo "Include Role test: before file was not created"
    exit 1
fi
if [ ! -f /tmp/include_role_task.txt ]; then
    echo "Include Role test: role task file was not created"
    exit 1
fi
if [ ! -f /tmp/include_role_after.txt ]; then
    echo "Include Role test: after file was not created"
    exit 1
fi

# Test 10: Import Tasks directive test
echo "Running import_tasks directive test..."
go run main.go generate -p $TESTS_DIR/playbooks/import_tasks_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/default.yaml

# Check if files from before, imported, and after were created
if [ ! -f /tmp/import_tasks_before.txt ]; then
    echo "Import Tasks test: before file was not created"
    exit 1
fi
if [ ! -f /tmp/import_tasks_imported.txt ]; then
    echo "Import Tasks test: imported file was not created"
    exit 1
fi
if [ ! -f /tmp/import_tasks_after.txt ]; then
    echo "Import Tasks test: after file was not created"
    exit 1
fi

# Test 11: Import Role directive test
echo "Running import_role directive test..."
go run main.go generate -p $TESTS_DIR/playbooks/import_role_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/default.yaml

# Check if files from before, inside (role task), and after the role import were created
if [ ! -f /tmp/import_role_before.txt ]; then
    echo "Import Role test: before file was not created"
    exit 1
fi
# Role task file is /tmp/include_role_task.txt (reused from include_role test)
if [ ! -f /tmp/include_role_task.txt ]; then
    echo "Import Role test: role task file was not created"
    exit 1
fi
if [ ! -f /tmp/import_role_after.txt ]; then
    echo "Import Role test: after file was not created"
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
./generated_tasks -config tests/configs/default.yaml
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
      shell: echo "Created by root-level tasks section" > /tmp/root_playbook_tasks.txt
EOF

go run main.go generate -p $TESTS_DIR/playbooks/root_tasks_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/default.yaml

# Check if the file was created
if [ ! -f /tmp/root_playbook_tasks.txt ]; then
    echo "Root-level tasks test: file was not created"
    exit 1
fi

# Verify file content
if ! grep -q "Created by root-level tasks section" /tmp/root_playbook_tasks.txt; then
    echo "Root-level tasks test: file has incorrect content"
    cat /tmp/root_playbook_tasks.txt # Print content for debugging
    exit 1
fi

# Test 14: Root-level playbook with roles section
echo "Running root-level playbook with roles test..."

go run main.go generate -p $TESTS_DIR/playbooks/root_roles_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/default.yaml

# Check if the file was created
if [ ! -f /tmp/root_playbook_role.txt ]; then
    echo "Root-level roles test: file was not created"
    exit 1
fi

# Verify file content
if ! grep -q "Created by root-level roles section" /tmp/root_playbook_role.txt; then
    echo "Root-level roles test: file has incorrect content"
    cat /tmp/root_playbook_role.txt # Print content for debugging
    exit 1
fi

# Test 15: Root-level playbook with both roles and tasks
echo "Running root-level playbook with both roles and tasks test..."

# First remove any existing file from previous test
rm -f /tmp/root_playbook_role.txt

go run main.go generate -p $TESTS_DIR/playbooks/root_both_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/sequential.yaml

# Check if the file was created
if [ ! -f /tmp/root_playbook_role.txt ]; then
    echo "Root-level both test: file was not created"
    exit 1
fi

# Verify file contains both the role output and the direct task output
if ! grep -q "Created by root-level roles section" /tmp/root_playbook_role.txt || ! grep -q "Additional task" /tmp/root_playbook_role.txt; then
    echo "Root-level both test: file doesn't contain expected content from both role and direct task"
    cat /tmp/root_playbook_role.txt # Print content for debugging
    exit 1
fi

# Test 16: Stat module test
echo "Running stat module test..."
go run main.go generate -p $TESTS_DIR/playbooks/stat_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/sequential.yaml
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
./generated_tasks -config tests/configs/sequential.yaml
COMMAND_EXIT_CODE=$?
set -e

# Check if the exit code indicates failure (should be non-zero)
if [ $COMMAND_EXIT_CODE -eq 0 ]; then
    echo "Command module test failed: Playbook succeeded unexpectedly (Exit Code: $COMMAND_EXIT_CODE). Revert likely did not trigger."
    exit 1
fi
echo "Command playbook failed as expected (Exit Code: $COMMAND_EXIT_CODE), checking results..."

# Check file existence
if [ ! -f /tmp/spage_command_test_file1.txt ]; then
    echo "Command test: file1 was not created"
    exit 1
fi
if [ ! -f /tmp/spage_command_test_file2.txt ]; then
    echo "Command test: file2 was not created"
    exit 1
fi
if [ ! -d /tmp/spage_command_dir ]; then
    echo "Command test: directory was not created"
    exit 1
fi

# Check for no shell expansion (file 3 created)
if [ ! -f /tmp/spage_command_test_no_expand.txt ]; then
    echo "Command test: no_expand file was not created"
    exit 1
fi

# Check revert worked (revert file should NOT exist)
if [ -f /tmp/spage_command_revert.txt ]; then # Check if file exists (it shouldn't)
    echo "Command test: revert failed, revert file still exists"
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
    ./generated_tasks -config tests/configs/sequential.yaml
    APT_EXIT_CODE=$?
    set -e

    # Check if the exit code indicates failure (should be non-zero)
    if [ $APT_EXIT_CODE -eq 0 ]; then
        echo "Apt module test failed: Playbook succeeded unexpectedly (Exit Code: $APT_EXIT_CODE). Revert likely did not trigger."
        exit 1
    fi
    echo "Apt playbook failed as expected (Exit Code: $APT_EXIT_CODE), checking results..."

    # Check that cowsay IS NOT installed (due to successful revert)
    if command -v cowsay &> /dev/null; then
        echo "Apt test failed: cowsay is still installed after revert should have removed it."
        exit 1
    fi
    # Check that sl IS NOT installed (due to successful revert)
    if command -v sl &> /dev/null; then
        echo "Apt test failed: sl is still installed after revert should have removed it."
        exit 1
    fi
    echo "Apt module test (including list install and revert) succeeded."
fi # End of apt-get check

# Test 19: When condition test
echo "Running when condition test..."
go run main.go generate -p $TESTS_DIR/playbooks/when_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/sequential.yaml
WHEN_EXIT_CODE=$?

if [ $WHEN_EXIT_CODE -ne 0 ]; then
    echo "When condition test failed: Playbook execution failed unexpectedly (Exit Code: $WHEN_EXIT_CODE)."
    exit 1
fi

# Check results: Expected files should exist, skipped files should not
if [ ! -f /tmp/spage_when_test_should_run.txt ]; then
    echo "When test failed: 'should_run' file was not created."
    exit 1
fi
if [ ! -f /tmp/spage_when_test_simple_true.txt ]; then
    echo "When test failed: 'simple_true' file was not created."
    exit 1
fi

if [ -f /tmp/spage_when_test_should_skip.txt ]; then
    echo "When test failed: 'should_skip' file was created but should have been skipped."
    exit 1
fi
if [ -f /tmp/spage_when_test_nonexistent.txt ]; then
    echo "When test failed: 'nonexistent' var file was created but should have been skipped."
    exit 1
fi
if [ -f /tmp/spage_when_test_simple_false.txt ]; then
    echo "When test failed: 'simple_false' file was created but should have been skipped."
    exit 1
fi
echo "When condition test succeeded."

# Test 20: Slurp module test
echo "Running slurp module test..."
go run main.go generate -p $TESTS_DIR/playbooks/slurp_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/sequential.yaml # Use sequential to ensure file creation before slurp
SLURP_EXIT_CODE=$?

if [ $SLURP_EXIT_CODE -ne 0 ]; then
    echo "Slurp module test failed: Playbook execution failed unexpectedly (Exit Code: $SLURP_EXIT_CODE)."
    exit 1
fi
echo "Slurp module test succeeded."

echo "All tests completed successfully!"

