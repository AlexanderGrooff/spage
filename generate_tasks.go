//go:build ignore
// +build ignore

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/AlexanderGrooff/spage/pkg"
	"gopkg.in/yaml.v3"
)

func main() {
	// Define command-line flag for YAML file path
	yamlFile := flag.String("file", "", "Path to the YAML file containing tasks")
	flag.Parse()

	if *yamlFile == "" {
		log.Fatal("Please provide a YAML file path using the -file flag")
	}

	// Read YAML file
	data, err := ioutil.ReadFile(*yamlFile)
	if err != nil {
		log.Fatalf("Error reading YAML file: %v", err)
	}

	// Parse YAML
	var tasks []pkg.Task
	err = yaml.Unmarshal(data, &tasks)
	if err != nil {
		log.Fatalf("Error parsing YAML: %v", err)
	}

	graph := pkg.NewGraph(tasks)

	// Generate Go code
	f, err := os.Create("generated/tasks.go")
	if err != nil {
		log.Fatalf("Error creating file: %v", err)
	}
	defer f.Close()

	fmt.Printf("Compiling graph to code: %s", graph)

	fmt.Fprintln(f, "package generated")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "import (")
	fmt.Fprintln(f, `    "github.com/AlexanderGrooff/spage/pkg"`)
	fmt.Fprintln(f, ")")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "var Graph = pkg.Graph{")
	fmt.Fprintln(f, "\tTasks: [][]pkg.Task{")

	for _, taskExecutionLevel := range graph.Tasks {
		fmt.Fprintln(f, "\t\t[]pkg.Task{")
		for _, task := range taskExecutionLevel {
			fmt.Fprintf(f, "\t\t\t{Name: %q, Module: %q, Params: map[string]interface{}{\n", task.Name, task.Module)
			for k, v := range task.Params {
				fmt.Fprintf(f, "\t\t\t\t%q: %#v,\n", k, v)
			}
			fmt.Fprintln(f, "\t\t\t}},")
		}
		fmt.Fprintln(f, "\t\t},")
	}

	fmt.Fprintln(f, "\t},")
	fmt.Fprintln(f, "}")
}
