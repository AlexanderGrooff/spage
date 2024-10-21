//go:build ignore
// +build ignore

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Task struct {
	Name   string                 `yaml:"name"`
	Module string                 `yaml:"module"`
	Params map[string]interface{} `yaml:"params"`
}

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
	var tasks []Task
	err = yaml.Unmarshal(data, &tasks)
	if err != nil {
		log.Fatalf("Error parsing YAML: %v", err)
	}

	// Generate Go code
	f, err := os.Create("generated/tasks.go")
	if err != nil {
		log.Fatalf("Error creating file: %v", err)
	}
	defer f.Close()

	fmt.Fprintln(f, "package generated")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "import (")
	fmt.Fprintln(f, `    "github.com/AlexanderGrooff/reconcile/cmd"`)
	fmt.Fprintln(f, ")")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "var GeneratedTasks = []cmd.Task{")

	for _, task := range tasks {
		fmt.Fprintf(f, "    {Name: %q, Module: %q, Params: map[string]interface{}{\n", task.Name, task.Module)
		for k, v := range task.Params {
			fmt.Fprintf(f, "        %q: %#v,\n", k, v)
		}
		fmt.Fprintln(f, "    }},")
	}

	fmt.Fprintln(f, "}")
}
