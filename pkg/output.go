package pkg

import "fmt"

// PPrintOutput prints the detailed output of a module, usually on error or debug.
func PPrintOutput(output ModuleOutput, err error) {
	if output != nil {
		fmt.Printf("    output: %s\n", output.String()) // Indent for clarity under status line
	}
	if err != nil {
		// Error is already printed in the status line (failed: ...)
		// We could log the error details again here at ERROR level if needed
		// LogError("Detailed error info", map[string]interface{}{"error": err})
	}
}
