//go:build ignore
// +build ignore

package main

import (
	"flag"
	"fmt"
	"log"
	"os"

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

	// Generate Go code
	f, err := os.Create("generated/tasks.go")
	if err != nil {
		log.Fatalf("Error creating file: %v", err)
	}
	defer f.Close()

	fmt.Printf("Compiling graph to code:\n%s", graph)

	fmt.Fprintln(f, "package generated")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "import (")
	fmt.Fprintln(f, `    "github.com/AlexanderGrooff/spage/pkg"`)
	fmt.Fprintln(f, `    "github.com/AlexanderGrooff/spage/pkg/modules"`)
	fmt.Fprintln(f, ")")
	fmt.Fprintln(f)
	fmt.Fprintf(f, graph.ToCode())
}
