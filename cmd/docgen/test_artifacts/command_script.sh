#!/bin/bash
set -e

# Create test file 1 using touch command
touch /tmp/spage/spage_command_test_file1.txt

# Create test file 2 using touch command
touch /tmp/spage/spage_command_test_file2.txt

# Create test file 3 using touch command (no shell expansion needed)
touch /tmp/spage/spage_command_test_no_expand.txt

# Create directory with mkdir
mkdir -p /tmp/spage/spage_command_dir

# Run command with loop equivalent
for item in first second third; do
    echo "loop item $item"
done

echo "All command operations completed successfully"
