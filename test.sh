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
    rm -rf /tmp/spage_test
    rm -f /tmp/step1.txt
    rm -f /tmp/step2.txt
    rm -f /tmp/revert_test.txt
    rm -f /tmp/revert_test.conf
    rm -rf /tmp/revert_test_dir
    echo "Cleanup complete"
}

# Set trap to call cleanup function on script exit
trap cleanup EXIT

# Test 1: Template playbook test
echo "Running template playbook test..."
go run generate_tasks.go -file $TESTS_DIR/playbooks/template_playbook.yaml
go run generated/tasks.go

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
go run generate_tasks.go -file $TESTS_DIR/playbooks/shell_playbook.yaml
go run generated/tasks.go

# Check if the test directory was created
if [ ! -d /tmp/spage_test ]; then
    echo "Shell test: test directory was not created"
    exit 1
fi

# Test 3: File operations test
echo "Running file operations test..."
go run generate_tasks.go -file $TESTS_DIR/playbooks/file_playbook.yaml
go run generated/tasks.go

# Check if test file exists with correct content
if [ ! -f /tmp/spage_test_file.txt ]; then
    echo "File operations test: test file was not created"
    exit 1
fi

echo "Running copy test..."
go run generate_tasks.go -file $TESTS_DIR/playbooks/copy_playbook.yaml
go run generated/tasks.go

# Test 4: Error handling test
echo "Running error handling test..."
if go run generate_tasks.go -file $TESTS_DIR/playbooks/invalid_playbook.yaml; then
    echo "Error handling test failed: should have errored on invalid playbook"
    exit 1
fi

# Test 5: Multi-step playbook test
echo "Running multi-step playbook test..."
go run generate_tasks.go -file $TESTS_DIR/playbooks/multi_step_playbook.yaml
go run generated/tasks.go

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
go run generate_tasks.go -file $TESTS_DIR/playbooks/revert_playbook.yaml

# Temporarily disable exit on error for the failing command
set +e
go run generated/tasks.go
REVERT_EXIT_CODE=$?
set -e

# Check if the exit code indicates failure (should be 1 for intentional failure)
# if [ $REVERT_EXIT_CODE -ne 1 ]; then
#     echo "Revert test failed: playbook should have failed with exit code 1, got $REVERT_EXIT_CODE"
#     exit 1
# fi

# Verify files were reverted to original state
if [ ! -f /tmp/revert_test.txt ] || [ "$(cat /tmp/revert_test.txt)" != "reverted" ]; then
    echo "Revert test failed: file content was not reverted"
    exit 1
fi

echo "All tests completed successfully!"

