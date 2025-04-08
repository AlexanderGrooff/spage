package pkg


// PPrintOutput prints the output and error in a pretty format
func PPrintOutput(output ModuleOutput, err error) {
	if err != nil {
		LogError("Task failed", map[string]interface{}{
			"error":  err,
			"output": output,
		})
		return
	}
	LogInfo("Task output", map[string]interface{}{
		"output": output,
	})
}
