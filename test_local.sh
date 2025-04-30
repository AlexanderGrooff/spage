#!/usr/bin/env bash

# Runs the main test suite against the default inventory (localhost).

set -e
set -x

SCRIPT_DIR=$(dirname "$0")


# --- Main Tests --- #
# Ensure SPAGE_INVENTORY is unset
unset SPAGE_INVENTORY

# Run the main test script
"$SCRIPT_DIR/test.sh"

# --- Post-Test Cleanup (Optional but good practice) --- #
rm -f "$CLEANUP_GO" "$CLEANUP_BIN"

echo "Local tests completed successfully!" 