#!/usr/bin/env bash

# Runs the main test suite against a specified remote inventory.

set -e
set -x

SCRIPT_DIR=$(dirname "$0")
INVENTORY_FILE="$SCRIPT_DIR/inventory.yaml"

if [ ! -f "$INVENTORY_FILE" ]; then
    echo "Error: Remote inventory file not found at $INVENTORY_FILE" >&2
    echo "Please create inventory.yaml in the script directory." >&2
    exit 1
fi

# --- Main Tests --- #
# Set the inventory environment variable
export SPAGE_INVENTORY="$INVENTORY_FILE"

# Run the main test script
"$SCRIPT_DIR/test.sh"

# --- Post-Test Cleanup (Optional but good practice) --- #
rm -f "$CLEANUP_GO" "$CLEANUP_BIN"

# Unset the variable after the test runs (optional, good practice)
unset SPAGE_INVENTORY

echo "Remote tests completed successfully!" 