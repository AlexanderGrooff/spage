package pkg

import (
	"fmt"
	"time"

	"github.com/pmezard/go-difflib/difflib"
)

// GenerateUnifiedDiff creates a unified diff string between two text contents.
func GenerateUnifiedDiff(filePath, originalContent, newContent string) (string, error) {
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(originalContent),
		B:        difflib.SplitLines(newContent),
		FromFile: fmt.Sprintf("%s (original)", filePath),
		ToFile:   fmt.Sprintf("%s (new)", filePath),
		Context:  3,
		Eol:      "\n",
		FromDate: time.Now().Format(time.RFC3339),
		ToDate:   time.Now().Format(time.RFC3339),
	}
	return difflib.GetUnifiedDiffString(diff)
}

// ShouldShowDiff determines if diff output should be displayed based on:
// 1. Global ansible_diff setting (set by --diff flag or task level setting)
// 2. Task-level diff setting (overrides global setting)
func ShouldShowDiff(closure *Closure) bool {
	// Check if ansible_diff is set in the closure (either globally or by task-level diff setting)
	if val, ok := closure.GetFact("ansible_diff"); ok {
		if diffVal, ok := val.(bool); ok {
			return diffVal
		}
	}
	return false
}
