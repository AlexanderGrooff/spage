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
