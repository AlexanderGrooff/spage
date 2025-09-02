package pkg

import (
	"testing"
)

func TestTaskCollectionIsGraphNode(t *testing.T) {
	var _ GraphNode = &TaskCollection{}
}
