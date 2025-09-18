#!/bin/bash

set -e

FILE=$1
PLAYBOOK=$2
NO_HEADER=$3

if [ -z "$FILE" ]; then
    echo "Usage: $0 <file> [playbook_name] [no_header]"
    echo "  file: benchmark output file"
    echo "  playbook_name: optional - specific playbook to analyze (File, Command, Jinja)"
    echo "                 if not provided, shows aggregate results"
    echo "  no_header: optional - if 'no_header', skip the section header"
    exit 1
fi

if [ ! -f "$FILE" ]; then
    echo "File not found: $FILE"
    exit 1
fi

# Function to extract method name from benchmark name
extract_method() {
    local benchmark_name=$1
    if [[ $benchmark_name == *"/ansible-"* ]]; then
        echo "ansible"
    elif [[ $benchmark_name == *"/bash-"* ]]; then
        echo "bash"
    elif [[ $benchmark_name == *"/spage_run_temporal-"* ]]; then
        echo "spage_run_temporal"
    elif [[ $benchmark_name == *"/spage_run-"* ]]; then
        echo "spage_run"
    elif [[ $benchmark_name == *"/go_run_generated-"* ]]; then
        echo "go_run_generated"
    elif [[ $benchmark_name == *"/compiled_binary-"* ]]; then
        echo "compiled_binary"
    else
        echo "unknown"
    fi
}

# Function to extract playbook name from benchmark name
extract_playbook() {
    local benchmark_name=$1
    if [[ $benchmark_name == *"/File/"* ]]; then
        echo "File"
    elif [[ $benchmark_name == *"/Command/"* ]]; then
        echo "Command"
    elif [[ $benchmark_name == *"/Jinja/"* ]]; then
        echo "Jinja"
    elif [[ $benchmark_name == *"FilePlaybook"* ]]; then
        echo "File"
    elif [[ $benchmark_name == *"CommandPlaybook"* ]]; then
        echo "Command"
    elif [[ $benchmark_name == *"JinjaPlaybook"* ]]; then
        echo "Jinja"
    else
        echo "unknown"
    fi
}

# Process benchmark results
if [ -n "$PLAYBOOK" ]; then
    # Filter for specific playbook
    if [ "$NO_HEADER" != "no_header" ]; then
        echo "### $PLAYBOOK operations"
        echo
    fi
    results=$(grep 'ns/op' $FILE | grep -i "$PLAYBOOK" | head -20 | while read line; do
        benchmark_name=$(echo "$line" | awk '{print $1}')
        ns_per_op=$(echo "$line" | awk '{print $3}')
        method=$(extract_method "$benchmark_name")
        ms=$(echo "$ns_per_op / 1000000" | bc)
        echo "$method $ms"
    done | awk '{arr[$1]+=$2; count[$1]++} END {for (i in arr) if(count[i]>0) print i, int(arr[i]/count[i])}')
else
    # Aggregate results across all playbooks
    echo "# Aggregate Benchmark Results (Average across all playbooks)"
    echo
    results=$(grep 'ns/op' $FILE | grep -E "(BenchmarkPlaybooks|Benchmark.*Playbook)" | while read line; do
        benchmark_name=$(echo "$line" | awk '{print $1}')
        ns_per_op=$(echo "$line" | awk '{print $3}')
        method=$(extract_method "$benchmark_name")
        playbook=$(extract_playbook "$benchmark_name")
        ms=$(echo "$ns_per_op / 1000000" | bc)
        echo "$method $ms $playbook"
    done | awk '{arr[$1]+=$2; count[$1]++} END {for (i in arr) if(count[i]>0) print i, int(arr[i]/count[i])}')
fi

if [ -z "$results" ]; then
    echo "No benchmark results found"
    exit 1
fi

max=$(echo "$results" | awk '{print $2}' | sort -n | tail -n 1)

# Format in Mermaid xy-chart format
echo "\`\`\`mermaid"
echo "---"
echo "config:"
echo "    xyChart:"
echo "        width: 900"
echo "        height: 600"
echo "        showDataLabel: true"
echo "        labelPadding: 0"
echo "---"
echo "xychart"
if [ -n "$PLAYBOOK" ]; then
    echo "    title \"$PLAYBOOK Playbook: Performance Comparison\""
else
    echo "    title \"Average Performance Comparison (All Playbooks)\""
fi
echo "    x-axis [Ansible, Bash, \"Spage Run\", \"Spage Temporal\", \"Go Generated\", \"Compiled Binary\"]"
echo "    y-axis \"Duration (ms)\" 0 --> $max"

ansible_time=$(echo "$results" | grep 'ansible' | awk '{print $2}' | head -1)
bash_time=$(echo "$results" | grep 'bash' | awk '{print $2}' | head -1)
spage_time=$(echo "$results" | grep -w 'spage_run' | awk '{print $2}' | head -1)
spage_temporal_time=$(echo "$results" | grep 'spage_run_temporal' | awk '{print $2}' | head -1)
go_time=$(echo "$results" | grep 'go_run_generated' | awk '{print $2}' | head -1)
compiled_time=$(echo "$results" | grep 'compiled_binary' | awk '{print $2}' | head -1)

# Default to 0 if not found
ansible_time=${ansible_time:-0}
bash_time=${bash_time:-0}
spage_time=${spage_time:-0}
spage_temporal_time=${spage_temporal_time:-0}
go_time=${go_time:-0}
compiled_time=${compiled_time:-0}

echo "    bar [$ansible_time, $bash_time, $spage_time, $spage_temporal_time, $go_time, $compiled_time]"
echo "\`\`\`"

# Print detailed table
echo
echo "#### Performance Comparison"
echo
echo "| Method | Duration (ms) | Factor vs Ansible |"
echo "|--------|---------------|-------------------|"

if [ "$ansible_time" -gt 0 ]; then
    echo "$results" | while read method time; do
        if [ -n "$time" ] && [ "$time" -gt 0 ]; then
            if [ "$method" = "ansible" ]; then
                printf "| %-20s | %13s | %17s |\n" "Ansible" "${time}" "1.0x"
            elif [ "$method" = "bash" ]; then
                factor=$(echo "scale=1; $ansible_time / $time" | bc)
                printf "| %-20s | %13s | %17.1fx |\n" "Bash" "${time}" "$factor"
            elif [ "$method" = "spage_run" ]; then
                factor=$(echo "scale=1; $ansible_time / $time" | bc)
                printf "| %-20s | %13s | %17.1fx |\n" "Spage Run" "${time}" "$factor"
            elif [ "$method" = "spage_run_temporal" ]; then
                factor=$(echo "scale=1; $ansible_time / $time" | bc)
                printf "| %-20s | %13s | %17.1fx |\n" "Spage Temporal" "${time}" "$factor"
            elif [ "$method" = "go_run_generated" ]; then
                factor=$(echo "scale=1; $ansible_time / $time" | bc)
                printf "| %-20s | %13s | %17.1fx |\n" "Go Generated" "${time}" "$factor"
            elif [ "$method" = "compiled_binary" ]; then
                factor=$(echo "scale=1; $ansible_time / $time" | bc)
                printf "| %-20s | %13s | %17.1fx |\n" "Compiled Binary" "${time}" "$factor"
            fi
        fi
    done
else
    echo "| No ansible results found | - | - |"
fi

# Show playbook breakdown if aggregate
if [ -z "$PLAYBOOK" ]; then
    echo
    echo "## Per-Playbook Breakdown"
    echo
    for pb in "File" "Command" "Jinja"; do
        echo "### $pb Playbook"
        pb_results=$(grep 'ns/op' $FILE | grep -i "$pb" | head -8 | while read line; do
            benchmark_name=$(echo "$line" | awk '{print $1}')
            ns_per_op=$(echo "$line" | awk '{print $3}')
            method=$(extract_method "$benchmark_name")
            ms=$(echo "$ns_per_op / 1000000" | bc)
            echo "$method: ${ms}ms"
        done)
        if [ -n "$pb_results" ]; then
            echo "$pb_results" | sort
        else
            echo "No results found for $pb playbook"
        fi
        echo
    done
fi
