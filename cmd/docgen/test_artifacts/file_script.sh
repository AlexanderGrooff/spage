#!/bin/bash
set -e

# Create a test file
touch /tmp/spage/spage_test_file.txt

# Write content to file
echo "This is a test file created by spage" > /tmp/spage/spage_test_file.txt

# Set file permissions
chmod 644 /tmp/spage/spage_test_file.txt

# Create symlink
ln -sf /tmp/spage/spage_test_file.txt /tmp/spage/spage_test_link.txt

# Stat the link and assert it exists
if [[ -L /tmp/spage/spage_test_link.txt ]]; then
    echo "Link exists and is a symlink"
else
    echo "ERROR: Link does not exist or is not a symlink"
    exit 1
fi

# Change link target
ln -sf /tmp/spage/non_existent_target.txt /tmp/spage/spage_test_link.txt

# Stat the link again and assert it exists
if [[ -L /tmp/spage/spage_test_link.txt ]]; then
    echo "Link target changed successfully"
else
    echo "ERROR: Link does not exist after target change"
    exit 1
fi

# Remove the link
rm -f /tmp/spage/spage_test_link.txt

# Assert link removed
if [[ ! -e /tmp/spage/spage_test_link.txt ]]; then
    echo "Link removed successfully"
else
    echo "ERROR: Link still exists after removal"
    exit 1
fi

# Test file creation with different permissions
touch /tmp/spage/spage_file_test_dest.txt
chmod 600 /tmp/spage/spage_file_test_dest.txt

# Stat file and verify it exists
if [[ -f /tmp/spage/spage_file_test_dest.txt ]]; then
    # Check permissions
    perms=$(stat -c "%a" /tmp/spage/spage_file_test_dest.txt)
    if [[ "$perms" == "600" ]]; then
        echo "File created with correct permissions"
    else
        echo "ERROR: File permissions are $perms, expected 600"
        exit 1
    fi
else
    echo "ERROR: File does not exist"
    exit 1
fi

# Test directory creation
mkdir -p /tmp/spage/spage_file_test_name.txt
chmod 750 /tmp/spage/spage_file_test_name.txt

# Stat directory and verify it exists
if [[ -d /tmp/spage/spage_file_test_name.txt ]]; then
    # Check permissions
    perms=$(stat -c "%a" /tmp/spage/spage_file_test_name.txt)
    if [[ "$perms" == "750" ]]; then
        echo "Directory created with correct permissions"
    else
        echo "ERROR: Directory permissions are $perms, expected 750"
        exit 1
    fi
else
    echo "ERROR: Directory does not exist"
    exit 1
fi

# Test link creation with different alias
ln -sf /tmp/spage/spage_test_file.txt /tmp/spage/spage_file_test_link_dest.txt

# Stat link and verify it exists
if [[ -L /tmp/spage/spage_file_test_link_dest.txt ]]; then
    echo "Link created successfully with dest alias"
else
    echo "ERROR: Link does not exist"
    exit 1
fi

echo "All file operations completed successfully"
