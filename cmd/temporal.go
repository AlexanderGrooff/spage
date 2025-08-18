package cmd

import (
	"fmt"
	"log"
	"maps"
	"os"

	"github.com/spf13/cobra"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/AlexanderGrooff/spage/pkg/executor"
)

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func StartTemporalExecutor(graph *pkg.Graph, inventoryFile string, spageAppConfig *config.Config, daemonClient interface{}) error {
	return StartTemporalExecutorWithLimit(graph, inventoryFile, spageAppConfig, daemonClient, "")
}

func StartTemporalExecutorWithLimit(graph *pkg.Graph, inventoryFile string, spageAppConfig *config.Config, daemonClient interface{}, limitPattern string) error {
	log.Printf("Preparing to run Temporal worker. Workflow trigger from config: %t", spageAppConfig.Temporal.Trigger)

	// Prepare options for the Temporal worker runner
	// Note: Temporal executor will handle limit filtering through ExecuteGraphWithLimit
	options := executor.RunSpageTemporalWorkerAndWorkflowOptions{
		Graph:            graph, // This is the graph code injected above
		InventoryPath:    inventoryFile,
		LoadedConfig:     spageAppConfig, // spageAppConfig now contains Temporal settings from config file, env, or defaults
		WorkflowIDPrefix: spageAppConfig.Temporal.WorkflowIDPrefix,
	}

	// Run the worker and potentially the workflow
	err := executor.RunSpageTemporalWorkerAndWorkflow(options)
	if err != nil {
		log.Printf("Error running Temporal worker: %v", err)
		return err
	}
	return nil
}

var (
	temporalConfigFile    string
	temporalInventoryFile string
	temporalCheckMode     bool
	temporalDiffMode      bool
	temporalTags          []string
	temporalSkipTags      []string
	temporalExtraVars     []string
	temporalBecomeMode    bool
)

func NewTemporalExecutorCmd(graph pkg.Graph) *cobra.Command {
	temporalCmd := &cobra.Command{
		Use:          "temporal",
		Short:        "Run the pre-compiled Spage playbook using Temporal",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Println("Starting Spage Temporal runner...")

			// Load Spage config
			spageConfigPath := temporalConfigFile
			if spageConfigPath == "" {
				spageConfigPath = "spage.yaml" // Default to spage.yaml if config flag is empty
				log.Printf("Config file flag is empty, attempting to load default: %s", spageConfigPath)
			}

			if err := LoadConfig(spageConfigPath); err != nil {
				// Only log a warning if a specific file was intended but failed, or if the default spage.yaml was tried and failed.
				if temporalConfigFile != "" || (temporalConfigFile == "" && spageConfigPath == "spage.yaml") {
					log.Printf("Warning: Failed to load Spage config file '%s': %v. Using internal defaults.", spageConfigPath, err)
				}
			} else {
				log.Printf("Spage config loaded from '%s'.", spageConfigPath)
			}
			spageAppConfig := GetConfig() // GetConfig() provides defaults if loading failed or no file specified

			if temporalCheckMode {
				if spageAppConfig.Facts == nil {
					spageAppConfig.Facts = make(map[string]interface{})
				}
				spageAppConfig.Facts["ansible_check_mode"] = true
			}

			if temporalDiffMode {
				if spageAppConfig.Facts == nil {
					spageAppConfig.Facts = make(map[string]interface{})
				}
				spageAppConfig.Facts["ansible_diff"] = true
			}

			// Parse and merge extra variables
			if len(temporalExtraVars) > 0 {
				if spageAppConfig.Facts == nil {
					spageAppConfig.Facts = make(map[string]interface{})
				}
				extraFacts, err := parseExtraVars(temporalExtraVars)
				if err != nil {
					return fmt.Errorf("failed to parse extra variables: %w", err)
				}
				// Merge extra facts into spageAppConfig.Facts (extra vars take precedence)
				maps.Copy(spageAppConfig.Facts, extraFacts)
			}

			// Apply become mode if enabled
			if temporalBecomeMode {
				applyBecomeToGraph(&graph)
			}

			// Apply tag filtering to the graph
			if len(temporalTags) > 0 {
				spageAppConfig.Tags.Tags = temporalTags
			}
			if len(temporalSkipTags) > 0 {
				spageAppConfig.Tags.SkipTags = temporalSkipTags
			}

			filteredGraph, err := applyTagFiltering(graph, spageAppConfig.Tags)
			if err != nil {
				return fmt.Errorf("failed to apply tag filtering: %w", err)
			}

			log.Printf("Preparing to run Temporal worker. Workflow trigger from config: %t", spageAppConfig.Temporal.Trigger)

			return StartTemporalExecutor(&filteredGraph, temporalInventoryFile, spageAppConfig, nil)
		},
	}

	temporalCmd.Flags().StringVarP(&temporalConfigFile, "config", "c", getEnv("SPAGE_CONFIG_FILE", ""), "Spage configuration file path. If empty, 'spage.yaml' is tried, then defaults.")
	temporalCmd.Flags().StringVarP(&temporalInventoryFile, "inventory", "i", getEnv("SPAGE_INVENTORY_FILE", ""), "Spage inventory file path. If empty, a default localhost inventory is used.")
	temporalCmd.Flags().BoolVar(&temporalCheckMode, "check", false, "Enable check mode (dry run)")
	temporalCmd.Flags().BoolVar(&temporalDiffMode, "diff", false, "Enable diff mode")
	temporalCmd.Flags().StringSliceVarP(&temporalTags, "tags", "t", []string{}, "Only include tasks with these tags (comma-separated)")
	temporalCmd.Flags().StringSliceVar(&temporalSkipTags, "skip-tags", []string{}, "Skip tasks with these tags (comma-separated)")
	temporalCmd.Flags().StringSliceVarP(&temporalExtraVars, "extra-vars", "e", []string{}, "Set additional variables as key=value or YAML/JSON")
	temporalCmd.Flags().BoolVar(&temporalBecomeMode, "become", false, "Run all tasks with become: true and become_user: root")

	return temporalCmd
}
