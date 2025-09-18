#!/bin/bash
set -e

# Set variables equivalent to Ansible vars
test_var="world"

# Concatenate lists - bash arrays
list_1=(1 2 3)
list_2=(4 5 6)

# Print set_fact_result equivalent
echo "first list: ${list_1[@]}, second list: ${list_2[@]}"

# Assert concatenate lists equivalent
# Check if list_1 equals [1, 2, 3]
expected_1=(1 2 3)
if [[ "${list_1[@]}" == "${expected_1[@]}" ]]; then
    echo "list_1 assertion passed"
else
    echo "ERROR: list_1 assertion failed"
    exit 1
fi

# Check if list_2 equals [4, 5, 6]
expected_2=(4 5 6)
if [[ "${list_2[@]}" == "${expected_2[@]}" ]]; then
    echo "list_2 assertion passed"
else
    echo "ERROR: list_2 assertion failed"
    exit 1
fi

# Concatenate arrays and check result
combined=("${list_1[@]}" "${list_2[@]}")
expected_combined=(1 2 3 4 5 6)
if [[ "${combined[@]}" == "${expected_combined[@]}" ]]; then
    echo "combined list assertion passed"
else
    echo "ERROR: combined list assertion failed"
    exit 1
fi

# Test omit behaviour - in bash, we can use parameter expansion with default
# If undefined_var is not set, this will echo nothing (equivalent to omit)
echo "${undefined_var:-}"

echo "All jinja operations completed successfully"
