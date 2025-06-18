package executor

import (
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
)

// TestTemporalGraphExecutorImplementsGraphExecutor verifies that TemporalGraphExecutor implements the GraphExecutor interface
func TestTemporalGraphExecutorImplementsGraphExecutor(t *testing.T) {
	// This is a compile-time check to ensure TemporalGraphExecutor implements GraphExecutor
	var _ pkg.GraphExecutor = (*TemporalGraphExecutor)(nil)
}
