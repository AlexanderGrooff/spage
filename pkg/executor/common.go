package executor

import (
	"fmt"
)

// GenericOutput is a flexible map-based implementation of pkg.ModuleOutput.
type GenericOutput map[string]interface{}

// Facts returns the output map itself, so all keys become facts.
func (g GenericOutput) Facts() map[string]interface{} {
	return g
}

// Changed checks for a "changed" key in the map.
func (g GenericOutput) Changed() bool {
	changed, ok := g["changed"].(bool)
	return ok && changed
}

// String provides a simple string representation of the map.
func (g GenericOutput) String() string {
	return fmt.Sprintf("%v", map[string]interface{}(g))
}
