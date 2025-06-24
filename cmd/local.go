package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/executor"
)

func StartLocalExecutor(graph pkg.Graph) {
	configFile := flag.String("config", "", "Config file path (default: ./spage.yaml)")
	inventoryFile := flag.String("inventory", "", "Inventory file path")
	flag.Parse()

	// Load configuration and apply logging settings
	err := LoadConfig(*configFile)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Execute the graph using the loaded configuration
	cfg := GetConfig()
	exec := executor.NewLocalGraphExecutor(&executor.LocalTaskRunner{})
	err = pkg.ExecuteGraph(exec, graph, *inventoryFile, cfg)
	if err != nil {
		fmt.Printf("Execution failed: %v\n", err)
		os.Exit(1)
	}
}
