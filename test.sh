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
./generated_tasks -config tests/configs/default.yaml

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

# Create test role
mkdir -p $TESTS_DIR/roles/test_root_role/tasks
cat > $TESTS_DIR/roles/test_root_role/tasks/main.yml << EOF
---
- name: Task from test_root_role
  shell: echo "Created by root-level roles section" > /tmp/root_playbook_role.txt
EOF

# Create test playbook
cat > $TESTS_DIR/playbooks/root_roles_playbook.yaml << EOF
---
- name: Root playbook with roles
  hosts: localhost
  roles:
    - test_root_role
EOF

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

# Create test playbook with both roles and tasks
cat > $TESTS_DIR/playbooks/root_both_playbook.yaml << EOF
---
- name: Root playbook with both roles and tasks
  hosts: localhost
  roles:
    - test_root_role
  tasks:
    - name: Additional task in playbook with roles
      shell: echo "Additional task" >> /tmp/root_playbook_role.txt
EOF

# First remove any existing file from previous test
rm -f /tmp/root_playbook_role.txt

go run main.go generate -p $TESTS_DIR/playbooks/root_both_playbook.yaml -o generated_tasks.go
go build -o generated_tasks generated_tasks.go
./generated_tasks -config tests/configs/default.yaml

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

echo "All tests completed successfully!"

