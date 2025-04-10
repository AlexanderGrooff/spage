package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/config"
	_ "github.com/AlexanderGrooff/spage/pkg/modules" // Register modules
)

var (
	playbookFile  string
	outputFile    string
	inventoryFile string
	hostname      string
	configFile    string
)

var LoadConfig = func(configFile string) error {
	// Load configuration
	configPaths := []string{}

	// If no config file specified, try to use spage.yaml from current directory
	if configFile == "" {
		defaultConfig := "spage.yaml"
		if _, err := os.Stat(defaultConfig); err == nil {
			configPaths = append(configPaths, defaultConfig)
		}
	} else {
		// Use specified config file
		configPaths = append(configPaths, configFile)
	}

	_, err := config.Load(configPaths...)
	if err != nil {
		// Only report error for missing config file if it was explicitly specified
		if configFile != "" || !os.IsNotExist(err) {
			return fmt.Errorf("failed to load configuration: %w", configPaths, err)
		}
		// If no config file found, proceed with defaults
		if _, err := config.Load(); err != nil {
			return fmt.Errorf("failed to load default configuration: %w", err)
		}
	}
	return nil
}

var RootCmd = &cobra.Command{
	Use:   "spage",
	Short: "Simple Playbook AGEnt",
	Long:  `A lightweight configuration management tool that compiles your playbooks into a Go program to run on a host.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return LoadConfig(configFile)
	},
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a graph from a playbook and save it as Go code",
	Run: func(cmd *cobra.Command, args []string) {
		graph, err := pkg.NewGraphFromFile(playbookFile)
		if err != nil {
			pkg.LogError("Failed to generate graph", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}

		graph.SaveToFile(outputFile)
		pkg.LogInfo("Compiled binary", map[string]interface{}{
			"output_file": outputFile,
		})
		// graph, err := pkg.NewGraphFromFile(playbookFile)
		// if err != nil {
		// 	fmt.Printf("Failed to generate graph: %s\n", err)
		// 	os.Exit(1)
		// }

		// compiledGraph, err := pkg.CompilePlaybookForHost(graph, inventoryFile, hostname)
		// if err != nil {
		// 	fmt.Printf("Failed to compile graph: %s\n", err)
		// 	os.Exit(1)
		// }
		// compiledGraph.SaveToFile(outputFile)
		// fmt.Printf("Compiled binary in %s\n", outputFile)
	},
}

func init() {
	// Add config flag to root command so it's available to all subcommands
	RootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "Config file path (default: ./spage.yaml)")

	generateCmd.Flags().StringVarP(&playbookFile, "playbook", "p", "", "Playbook file (required)")
	// generateCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file (required)")
	// generateCmd.Flags().StringVarP(&hostname, "hostname", "H", "", "Hostname (required)")
	generateCmd.Flags().StringVarP(&outputFile, "output", "o", "generated_tasks.go", "Output file (default: generated_tasks.go)")

	generateCmd.MarkFlagRequired("playbook")
	// generateCmd.MarkFlagRequired("inventory")
	// generateCmd.MarkFlagRequired("hostname")

	RootCmd.AddCommand(generateCmd)
}
