package cmd

import (
	"flag"
	"fmt"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/AlexanderGrooff/spage/pkg/executor"
)

func StartLocalExecutor(graph pkg.Graph, inventoryFile string, cfg *config.Config) error {
	exec := executor.NewLocalGraphExecutor(&executor.LocalTaskRunner{})
	err := pkg.ExecuteGraph(exec, graph, inventoryFile, cfg)
	if err != nil {
		fmt.Printf("Execution failed: %v\n", err)
		return err
	}
	return nil
}

func EntrypointLocalExecutor(graph pkg.Graph) error {
	configFile := flag.String("config", "", "Config file path (default: ./spage.yaml)")
	inventoryFile := flag.String("inventory", "", "Inventory file path")
	checkMode := flag.Bool("check", false, "Enable check mode (dry run)")
	diffMode := flag.Bool("diff", false, "Enable diff mode")
	flag.Parse()

	// Load configuration and apply logging settings
	err := LoadConfig(*configFile)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return err
	}

	// Execute the graph using the loaded configuration
	cfg := GetConfig()
	if *checkMode {
		if cfg.Facts == nil {
			cfg.Facts = make(map[string]interface{})
		}
		cfg.Facts["ansible_check_mode"] = true
	}
	if *diffMode {
		if cfg.Facts == nil {
			cfg.Facts = make(map[string]interface{})
		}
		cfg.Facts["ansible_diff"] = true
	}

	return StartLocalExecutor(graph, *inventoryFile, cfg)
}
