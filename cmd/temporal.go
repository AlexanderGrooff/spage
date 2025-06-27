package cmd

import (
	"flag"
	"log"
	"os"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/executor"
)

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func StartTemporalExecutor(graph pkg.Graph) {
	log.Println("Starting Spage Temporal runner...")

	// Define flags with environment variable fallbacks
	configFile := flag.String("config", getEnv("SPAGE_CONFIG_FILE", ""), "Spage configuration file path. If empty, 'spage.yaml' is tried, then defaults.")
	inventoryFile := flag.String("inventory", getEnv("SPAGE_INVENTORY_FILE", ""), "Spage inventory file path. If empty, a default localhost inventory is used.")
	checkMode := flag.Bool("check", false, "Enable check mode (dry run)")
	diffMode := flag.Bool("diff", false, "Enable diff mode")

	flag.Parse()

	// Load Spage config
	spageConfigPath := *configFile
	if spageConfigPath == "" {
		spageConfigPath = "spage.yaml" // Default to spage.yaml if config flag is empty
		log.Printf("Config file flag is empty, attempting to load default: %s", spageConfigPath)
	}

	if err := LoadConfig(spageConfigPath); err != nil {
		// Only log a warning if a specific file was intended but failed, or if the default spage.yaml was tried and failed.
		if *configFile != "" || (*configFile == "" && spageConfigPath == "spage.yaml") {
			log.Printf("Warning: Failed to load Spage config file '%s': %v. Using internal defaults.", spageConfigPath, err)
		}
	} else {
		log.Printf("Spage config loaded from '%s'.", spageConfigPath)
	}
	spageAppConfig := GetConfig() // GetConfig() provides defaults if loading failed or no file specified

	if *checkMode {
		if spageAppConfig.Facts == nil {
			spageAppConfig.Facts = make(map[string]interface{})
		}
		spageAppConfig.Facts["ansible_check_mode"] = true
	}

	if *diffMode {
		if spageAppConfig.Facts == nil {
			spageAppConfig.Facts = make(map[string]interface{})
		}
		spageAppConfig.Facts["ansible_diff"] = true
	}

	log.Printf("Preparing to run Temporal worker. Workflow trigger from config: %t", spageAppConfig.Temporal.Trigger)

	// Prepare options for the Temporal worker runner
	options := executor.RunSpageTemporalWorkerAndWorkflowOptions{
		Graph:            &graph, // This is the graph code injected above
		InventoryPath:    *inventoryFile,
		LoadedConfig:     spageAppConfig, // spageAppConfig now contains Temporal settings from config file, env, or defaults
		WorkflowIDPrefix: spageAppConfig.Temporal.WorkflowIDPrefix,
	}

	// Run the worker and potentially the workflow
	executor.RunSpageTemporalWorkerAndWorkflow(options)
}
