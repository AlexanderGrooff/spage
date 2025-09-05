package pkg

import (
	"testing"
)

func TestMetaTaskIsGraphNode(t *testing.T) {
	var _ GraphNode = &MetaTask{}
}
