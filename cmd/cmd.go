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
	configFile    string
	tags          []string
	skipTags      []string
	cfg           *config.Config // Store the loaded config
	checkMode     bool
	diffMode      bool
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

func GetGraph(playbookFile string, tags, skipTags []string, baseConfig *config.Config) (pkg.Graph, error) {
	// Override config with command line flags if provided
	if len(tags) > 0 {
		baseConfig.Tags.Tags = tags
	}
	if len(skipTags) > 0 {
		baseConfig.Tags.SkipTags = skipTags
	}

	graph, err := pkg.NewGraphFromFile(playbookFile, baseConfig.RolesPath)
	if err != nil {
		return pkg.Graph{}, fmt.Errorf("failed to generate graph from playbook: %w", err)
	}

	// Apply tag filtering to the graph
	filteredGraph, err := applyTagFiltering(graph, baseConfig.Tags)
	if err != nil {
		return pkg.Graph{}, fmt.Errorf("failed to apply tag filtering: %w", err)
	}
	return filteredGraph, nil
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a graph from a playbook and save it as Go code",
	RunE: func(cmd *cobra.Command, args []string) error {
		graph, err := GetGraph(playbookFile, tags, skipTags, cfg)
		if err != nil {
			common.LogError("Failed to generate graph", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}

		if cfg.Executor == "temporal" {
			err = graph.SaveToTemporalWorkflowFile(outputFile)
		} else {
			err = graph.SaveToFile(outputFile)
		}
		common.LogInfo("Compiled binary", map[string]interface{}{
			"output_file": outputFile,
		})
		if err != nil {
			common.LogError("Failed to generate graph", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}
		return nil
	},
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a playbook by compiling & executing it",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate that either inventory file is provided or inventory paths are configured
		if inventoryFile == "" && (cfg == nil || cfg.Inventory == "") {
			return fmt.Errorf("either --inventory flag must be provided or inventory paths must be configured")
		}

		graph, err := GetGraph(playbookFile, tags, skipTags, cfg)
		if err != nil {
			common.LogError("Failed to generate graph", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}
		if checkMode {
			if cfg.Facts == nil {
				cfg.Facts = make(map[string]interface{})
			}
			cfg.Facts["ansible_check_mode"] = true
		}
		if diffMode {
			if cfg.Facts == nil {
				cfg.Facts = make(map[string]interface{})
			}
			cfg.Facts["ansible_diff"] = true
		}

		if cfg.Executor == "temporal" {
			err = StartTemporalExecutor(graph, inventoryFile, cfg)
		} else {
			err = StartLocalExecutor(graph, inventoryFile, cfg)
		}
		if err != nil {
			common.LogError("Failed to run playbook", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	// Add config flag to root command so it's available to all subcommands
	RootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "Config file path (default: ./spage.yaml)")

	generateCmd.Flags().StringVarP(&playbookFile, "playbook", "p", "", "Playbook file (required)")
	generateCmd.Flags().StringVarP(&outputFile, "output", "o", "generated_tasks.go", "Output file (default: generated_tasks.go)")
	generateCmd.Flags().StringSliceVarP(&tags, "tags", "t", []string{}, "Only include tasks with these tags (comma-separated)")
	generateCmd.Flags().StringSliceVar(&skipTags, "skip-tags", []string{}, "Skip tasks with these tags (comma-separated)")

	if err := generateCmd.MarkFlagRequired("playbook"); err != nil {
		panic(fmt.Sprintf("failed to mark playbook flag as required: %v", err))
	}

	runCmd.Flags().StringVarP(&playbookFile, "playbook", "p", "", "Playbook file (required)")
	runCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file (optional if inventory paths configured)")
	runCmd.Flags().StringVarP(&outputFile, "output", "o", "generated_tasks.go", "Output file (default: generated_tasks.go)")
	runCmd.Flags().StringSliceVarP(&tags, "tags", "t", []string{}, "Only include tasks with these tags (comma-separated)")
	runCmd.Flags().StringSliceVar(&skipTags, "skip-tags", []string{}, "Skip tasks with these tags (comma-separated)")
	runCmd.Flags().BoolVar(&checkMode, "check", false, "Enable check mode (dry run)")
	runCmd.Flags().BoolVar(&diffMode, "diff", false, "Enable diff mode")

	if err := runCmd.MarkFlagRequired("playbook"); err != nil {
		panic(fmt.Sprintf("failed to mark playbook flag as required: %v", err))
	}

	RootCmd.AddCommand(generateCmd)
	RootCmd.AddCommand(runCmd)
}

// GetConfig returns the loaded configuration
func GetConfig() *config.Config {
	return cfg
}

// applyTagFiltering filters tasks based on tag configuration
func applyTagFiltering(graph pkg.Graph, tagsConfig config.TagsConfig) (pkg.Graph, error) {
	// Always apply filtering to handle special tags like "never" correctly
	// Even when no specific tags are requested, "never" tagged tasks should be excluded

	filteredGraph := pkg.Graph{
		RequiredInputs: graph.RequiredInputs, // Copy required inputs
		Tasks:          make([][]pkg.Task, 0),
		Vars:           graph.Vars,
	}

	for _, taskLayer := range graph.Tasks {
		var filteredLayer []pkg.Task
		for _, task := range taskLayer {
			if shouldIncludeTask(task, tagsConfig) {
				filteredLayer = append(filteredLayer, task)
			}
		}
		// Only add the layer if it has tasks
		if len(filteredLayer) > 0 {
			filteredGraph.Tasks = append(filteredGraph.Tasks, filteredLayer)
		}
	}

	return filteredGraph, nil
}

// shouldIncludeTask determines if a task should be included based on its tags
func shouldIncludeTask(task pkg.Task, tagsConfig config.TagsConfig) bool {
	taskTags := task.Tags

	// Special tag handling: "always" tag means always include (unless skipped)
	hasAlwaysTag := contains(taskTags, "always")

	// Special tag handling: "never" tag means never include (unless explicitly tagged)
	hasNeverTag := contains(taskTags, "never")

	// Check if task should be skipped
	if len(tagsConfig.SkipTags) > 0 {
		for _, skipTag := range tagsConfig.SkipTags {
			if contains(taskTags, skipTag) {
				return false // Skip this task
			}
		}
	}

	// Handle "never" tag: only include if explicitly requested
	if hasNeverTag {
		if len(tagsConfig.Tags) == 0 {
			// No specific tags requested, exclude "never" tasks
			return false
		}
		// Check if "never" tag is explicitly requested
		hasMatchingTag := false
		for _, wantedTag := range tagsConfig.Tags {
			if contains(taskTags, wantedTag) {
				hasMatchingTag = true
				break
			}
		}
		if !hasMatchingTag {
			return false
		}
	}

	// If task has "always" tag, include it (unless skipped above)
	if hasAlwaysTag {
		return true
	}

	// If no specific tags requested, include all tasks (except "never" which was handled above)
	if len(tagsConfig.Tags) == 0 {
		return true
	}

	// Check if task has any of the requested tags
	for _, wantedTag := range tagsConfig.Tags {
		if contains(taskTags, wantedTag) {
			return true
		}
	}

	// If task has no tags but we're filtering by tags, exclude it
	if len(taskTags) == 0 {
		return false
	}

	// Task doesn't match any wanted tags
	return false
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
