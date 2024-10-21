package cmd

import (
	"fmt"
	"os"

	"github.com/AlexanderGrooff/reconcile/modules"
	"gopkg.in/yaml.v3"
)

func reconcile(file string, inventoryFile string) error {
	fmt.Printf("Reconciling file: %s\n", file)
	fmt.Printf("Using inventory file: %s\n", inventoryFile)

	yamlContent, err := readYAMLFile(file)
	if err != nil {
		return fmt.Errorf("failed to read YAML file: %w", err)
	}
	fmt.Printf("YAML content: %v\n", yamlContent)

	for i, task := range yamlContent {
		module, ok := modules.GetModule(task.Module)
		if !ok {
			return fmt.Errorf("module %s not found", task.Module)
		}

		fmt.Printf("Executing task %d: %s\n", i+1, task.Name)
		err := module.Execute(task.Params)
		if err != nil {
			fmt.Printf("Error executing task %d: %v\n", i+1, err)
			fmt.Println("Reverting tasks...")
			
			// Revert tasks in reverse order
			for j := i; j >= 0; j-- {
				revertModule, _ := modules.GetModule(yamlContent[j].Module)
				fmt.Printf("Reverting task %d: %s\n", j+1, yamlContent[j].Name)
				if revertErr := revertModule.Revert(yamlContent[j].Params); revertErr != nil {
					fmt.Printf("Error reverting task %d: %v\n", j+1, revertErr)
				}
			}
			
			return fmt.Errorf("reconciliation failed: %w", err)
		}
	}

	fmt.Println("All tasks executed successfully")
	return nil
}

func readYAMLFile(filename string) ([]Task, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var result []Task
	if err := yaml.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML: %w", err)
	}
	return result, nil
}
