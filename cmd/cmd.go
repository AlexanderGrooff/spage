package cmd

import (
	"fmt"
	"os"

	"github.com/AlexanderGrooff/spage/pkg/common"

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
	cfg           *config.Config // Store the loaded config
)

// LoadConfig loads the configuration and applies settings
var LoadConfig = func(configFile string) error {
	configPaths := []string{}
	if configFile == "" {
		defaultConfig := "spage.yaml"
		if _, err := os.Stat(defaultConfig); err == nil {
			configPaths = append(configPaths, defaultConfig)
		}
	} else {
		configPaths = append(configPaths, configFile)
	}

	var err error
	cfg, err = config.Load(configPaths...)
	if err != nil {
		if configFile != "" || !os.IsNotExist(err) {
			return fmt.Errorf("failed to load configuration from %v: %w", configPaths, err)
		}
		// If no specific config file found/specified, try loading defaults
		cfg, err = config.Load() // Load defaults
		if err != nil {
			return fmt.Errorf("failed to load default configuration: %w", err)
		}
	}

	// Apply logging configuration AFTER loading config
	common.SetLogLevel(cfg.Logging.Level)
	if cfg.Logging.File != "" {
		if err := common.SetLogFile(cfg.Logging.File); err != nil {
			return fmt.Errorf("error setting log file: %w", err)
		}
	}
	// Call SetLogFormat with the loaded logging config
	if err := common.SetLogFormat(cfg.Logging); err != nil {
		return fmt.Errorf("error setting log format: %w", err)
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
			common.LogError("Failed to generate graph", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}

		graph.SaveToTemporalWorkflowFile(outputFile)
		common.LogInfo("Compiled binary", map[string]interface{}{
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

// GetConfig returns the loaded configuration
func GetConfig() *config.Config {
	return cfg
}
