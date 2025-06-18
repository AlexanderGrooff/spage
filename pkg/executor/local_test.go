package executor

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
)

// TestLocalGraphExecutorImplementsGraphExecutor verifies that LocalGraphExecutor implements the GraphExecutor interface
func TestLocalGraphExecutorImplementsGraphExecutor(t *testing.T) {
	// This is a compile-time check to ensure LocalGraphExecutor implements GraphExecutor
	var _ pkg.GraphExecutor = (*LocalGraphExecutor)(nil)
}
