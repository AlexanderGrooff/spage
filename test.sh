#!/usr/bin/env bash

# This script is used to test the spage project by running the spage command on several fixtures.
# This makes mutations on the host it is run on, so be careful when running it.
# Most of it is in the fixtures directory.

set -e
set -x

TESTS_DIR=$(dirname "$0")/tests

go run generate_tasks.go -file $TESTS_DIR/playbooks/template_playbook.yaml
go run generated/tasks.go

# Check if the file was created
if [ ! -f ./test.conf ]; then
    echo "File was not created"
    exit 1
fi
if [ ! -f /tmp/test.conf ]; then
    echo "File was not created"
    exit 1
fi

