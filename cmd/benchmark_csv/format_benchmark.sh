#!/bin/bash

set -e

FILE=$1

if [ -z "$FILE" ]; then
    echo "Usage: $0 <file>"
    exit 1
fi

if [ ! -f "$FILE" ]; then
    echo "File not found: $FILE"
    exit 1
fi

results=$(grep 'ns/op' $FILE | awk '{print$1 ";" $3}' | awk -F';' '{arr[$1]+=int($2 / 1000000)} END {for (i in arr) print i, arr[i]}')
max=$(echo "$results" | awk '{print $2}' | sort -n | tail -n 1)

# Format in Mermaid xy-chart format
echo "\`\`\`mermaid"
echo "---"
echo "config:"
echo "    xyChart:"
echo "        showDataLabel: true"
echo "---"
echo "xychart"
echo "  title \"Benchmark Spage vs Ansible\""
echo "  x-axis [\"ansible\",\"spage_run\",\"go_run_generated_tasks\",\"compiled_tasks\"]"
echo "  y-axis \"Duration (ms)\" 0 --> $max"

echo "  bar [$(echo "$results" | grep 'ansible' | awk '{print $2}'), $(echo "$results" | grep 'spage_run' | awk '{print $2}'), $(echo "$results" | grep 'go_run' | awk '{print $2}'), $(echo "$results" | grep 'compiled_tasks' | awk '{print $2}')]"
echo "\`\`\`"

# Resulting chart:
# xychart
#   title "Benchmark Spage vs Ansible"
#   x-axis ["ansible","spage_run","go_run_generated_tasks","compiled_tasks"]
#   y-axis "Duration (ms)" 0 --> 11548
#   bar [11548, 126, 466, 32]


# Print table showing factor differences
echo -e "\nFactor differences:"
echo "| Method | Duration (ms) | Factor vs Ansible |"
echo "|--------|--------------|------------------|"

ansible_time=$(echo "$results" | grep 'ansible' | awk '{print $2}')

echo "$results" | while read method time; do
    if [ -n "$time" ]; then
        factor=$(echo "scale=1; $ansible_time / $time" | bc)
        printf "| %-30s | %12d | %16.1fx |\n" "$method" "$time" "$factor"
    fi
done
