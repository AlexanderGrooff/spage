//go:build ignore
// +build ignore

package main

import (
	"flag"
	"log"

	"github.com/AlexanderGrooff/spage/pkg"
	_ "github.com/AlexanderGrooff/spage/pkg/modules"
)

func main() {
	// Define command-line flag for YAML file path
	yamlFile := flag.String("file", "", "Path to the YAML file containing tasks")
	flag.Parse()

	if *yamlFile == "" {
		log.Fatal("Please provide a YAML file path using the -file flag")
	}
	graph, err := pkg.NewGraphFromFile(*yamlFile)
	if err != nil {
		log.Fatalf("Failed to generate graph: %s", err)
	}

	graph.SaveToFile("generated/tasks.go")
}
