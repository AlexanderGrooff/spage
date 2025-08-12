package cmd

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/daemon"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

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
	extraVars     []string
	becomeMode    bool

	// Daemon communication flags
	daemonGRPC string
	playID     string
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

func GetGraph(playbookFile string, tags, skipTags []string, baseConfig *config.Config, becomeMode bool) (pkg.Graph, error) {
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

	// Apply become mode if enabled
	if becomeMode {
		applyBecomeToGraph(&graph)
	}

	// Apply tag filtering to the graph
	filteredGraph, err := applyTagFiltering(graph, baseConfig.Tags)
	if err != nil {
		return pkg.Graph{}, fmt.Errorf("failed to apply tag filtering: %w", err)
	}
	return filteredGraph, nil
}

func GetDaemonClient() (*daemon.Client, error) {
	// Initialize daemon client if daemon communication is enabled
	var daemonClient *daemon.Client
	var err error

	// Check if daemon communication is enabled via config or CLI flags
	daemonEnabled := cfg.Daemon.Enabled || daemonGRPC != "" || playID != ""

	if daemonEnabled {
		// Determine daemon endpoint (CLI flag takes precedence over config)
		daemonEndpoint := daemonGRPC
		if daemonEndpoint == "" {
			daemonEndpoint = cfg.Daemon.Endpoint
		}
		if daemonEndpoint == "" {
			daemonEndpoint = "localhost:9091"
		}

		// Determine play ID (CLI flag takes precedence over config)
		playIDToUse := playID
		if playIDToUse == "" {
			playIDToUse = cfg.Daemon.PlayID
		}
		if playIDToUse == "" {
			generatedTaskID := uuid.New().String()
			common.LogInfo("No play ID provided, generating a new one", map[string]interface{}{
				"play_id": generatedTaskID,
			})
			playIDToUse = generatedTaskID
		}

		daemonClient, err = daemon.NewClient(&daemon.Config{
			Endpoint: daemonEndpoint,
			TaskID:   playIDToUse,
			Timeout:  cfg.Daemon.Timeout,
		})
		if err != nil {
			common.LogError("Failed to create daemon client", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}
	}

	return daemonClient, nil
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a graph from a playbook and save it as Go code",
	RunE: func(cmd *cobra.Command, args []string) error {
		graph, err := GetGraph(playbookFile, tags, skipTags, cfg, becomeMode)
		if err != nil {
			common.LogError("Failed to generate graph", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}

		err = graph.SaveToFile(outputFile)
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
		daemonClient, err := GetDaemonClient()
		if err != nil {
			return fmt.Errorf("failed to get daemon client: %w", err)
		}

		graph, err := GetGraph(playbookFile, tags, skipTags, cfg, becomeMode)
		if err != nil {
			if daemonClient != nil {
				_ = daemonClient.RegisterPlayError()
			}
			return fmt.Errorf("failed to generate graph: %w", err)
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

		// Parse and merge extra variables
		if len(extraVars) > 0 {
			if cfg.Facts == nil {
				cfg.Facts = make(map[string]interface{})
			}
			extraFacts, err := parseExtraVars(extraVars)
			if err != nil {
				return fmt.Errorf("failed to parse extra variables: %w", err)
			}
			// Merge extra facts into cfg.Facts (extra vars take precedence)
			maps.Copy(cfg.Facts, extraFacts)

		}

		if cfg.Executor == "temporal" {
			err = StartTemporalExecutor(&graph, inventoryFile, cfg, daemonClient)
		} else {
			err = StartLocalExecutor(&graph, inventoryFile, cfg, daemonClient)
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
	generateCmd.Flags().BoolVar(&becomeMode, "become", false, "Run all tasks with become: true and become_user: root")

	if err := generateCmd.MarkFlagRequired("playbook"); err != nil {
		panic(fmt.Sprintf("failed to mark playbook flag as required: %v", err))
	}

	runCmd.Flags().StringVarP(&playbookFile, "playbook", "p", "", "Playbook file (required)")
	runCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file (default: localhost)")
	runCmd.Flags().StringVarP(&outputFile, "output", "o", "generated_tasks.go", "Output file (default: generated_tasks.go)")
	runCmd.Flags().StringSliceVarP(&tags, "tags", "t", []string{}, "Only include tasks with these tags (comma-separated)")
	runCmd.Flags().StringSliceVar(&skipTags, "skip-tags", []string{}, "Skip tasks with these tags (comma-separated)")
	runCmd.Flags().BoolVar(&checkMode, "check", false, "Enable check mode (dry run)")
	runCmd.Flags().BoolVar(&diffMode, "diff", false, "Enable diff mode")
	runCmd.Flags().StringSliceVarP(&extraVars, "extra-vars", "e", []string{}, "Set additional variables as key=value or YAML/JSON, i.e. -e 'key1=value1' -e 'key2=value2' or -e '{\"key1\": \"value1\", \"key2\": \"value2\"}'")
	runCmd.Flags().BoolVar(&becomeMode, "become", false, "Run all tasks with become: true and become_user: root")

	// Daemon communication flags
	runCmd.Flags().StringVar(&daemonGRPC, "daemon-grpc", "", "Daemon gRPC endpoint (default: localhost:9091)")
	runCmd.Flags().StringVar(&playID, "play-id", "", "Play ID for daemon communication")

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

// applyBecomeToGraph applies become: true and become_user: root to all tasks in the graph
func applyBecomeToGraph(graph *pkg.Graph) {
	for i := range graph.Tasks {
		for j := range graph.Tasks[i] {
			graph.Tasks[i][j].Become = true
			graph.Tasks[i][j].BecomeUser = "root"
		}
	}
}

// applyTagFiltering filters tasks based on tag configuration
func applyTagFiltering(graph pkg.Graph, tagsConfig config.TagsConfig) (pkg.Graph, error) {
	// Always apply filtering to handle special tags like "never" correctly
	// Even when no specific tags are requested, "never" tagged tasks should be excluded

	filteredGraph := pkg.Graph{
		RequiredInputs: graph.RequiredInputs, // Copy required inputs
		Tasks:          make([][]pkg.Task, 0),
		Handlers:       graph.Handlers, // Copy handlers
		Vars:           graph.Vars,
		PlaybookPath:   graph.PlaybookPath, // Copy base path
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

// parseExtraVars parses extra variables from command line arguments.
// Supports both key=value format and JSON/YAML format.
func parseExtraVars(extraVars []string) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for _, extraVar := range extraVars {
		// Check if it's a key=value format
		if strings.Contains(extraVar, "=") {
			parts := strings.SplitN(extraVar, "=", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid key=value format: %s", extraVar)
			}
			key := parts[0]
			value := parts[1]

			// Try to parse as JSON first, then as string
			var parsedValue interface{}
			if err := json.Unmarshal([]byte(value), &parsedValue); err == nil {
				result[key] = parsedValue
			} else {
				// If not valid JSON, treat as string
				result[key] = value
			}
		} else {
			// Try to parse as JSON/YAML object
			var parsedMap map[string]interface{}
			if err := json.Unmarshal([]byte(extraVar), &parsedMap); err == nil {
				// Merge the parsed map into result
				for k, v := range parsedMap {
					result[k] = v
				}
			} else {
				// Try YAML parsing
				if err := yaml.Unmarshal([]byte(extraVar), &parsedMap); err == nil {
					// Merge the parsed map into result
					for k, v := range parsedMap {
						result[k] = v
					}
				} else {
					return nil, fmt.Errorf("invalid extra variable format: %s (expected key=value or JSON/YAML object)", extraVar)
				}
			}
		}
	}

	return result, nil
}
