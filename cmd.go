//go:generate go run generate_tasks.go -file playbook.yaml

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AlexanderGrooff/spage/generated"
	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/generator"
)

var (
	playbookFile  string
	outputFile    string
	inventoryFile string
	hostname      string
)

var RootCmd = &cobra.Command{
	Use:   "spage",
	Short: "Simple Playbook AGEnt",
	Long:  `A lightweight configuration management tool that compiles your playbooks into a single binary per host.`,
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the playbook",
	Run: func(cmd *cobra.Command, args []string) {
		if err := pkg.Execute(generated.GeneratedGraph, inventoryFile); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a graph from a playbook and save it as Go code",
	Run: func(cmd *cobra.Command, args []string) {
		// gen := generator.NewGenerator(db)
		// graphPath, err := gen.GenerateGraphFromPlaybook(playbookFile, outputFile)
		// if err != nil {
		// 	fmt.Println(err)
		// 	os.Exit(1)
		// }
		// fmt.Printf("Saved generated graph in %s\n", graphPath)
		graph, err := pkg.NewGraphFromFile(playbookFile)
		if err != nil {
			fmt.Printf("Failed to generate graph: %s\n", err)
			os.Exit(1)
		}

		compiledGraph, err := pkg.CompilePlaybookForHost(graph, inventoryFile, hostname)
		if err != nil {
			fmt.Printf("Failed to compile graph: %s\n", err)
			os.Exit(1)
		}
		binaryPath, err := generator.BuildBinaryFromGraphForHost(&compiledGraph, outputFile, inventoryFile, hostname)
		if err != nil {
			fmt.Printf("Failed to build binary: %s\n", err)
			os.Exit(1)
		}
		fmt.Printf("Compiled binary in %s\n", binaryPath)
	},
}

var compileCmd = &cobra.Command{
	Use:   "compile",
	Short: "Compile a graph and inventory to a binary for a specific host",
	Run: func(cmd *cobra.Command, args []string) {
		compiledGraph, err := pkg.CompilePlaybookForHost(generated.GeneratedGraph, inventoryFile, hostname)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		binaryPath, err := generator.BuildBinaryFromGraphForHost(&compiledGraph, outputFile, inventoryFile, hostname)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Printf("Compiled binary in %s\n", binaryPath)
	},
}

func init() {
	runCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file (required)")
	generateCmd.Flags().StringVarP(&playbookFile, "playbook", "p", "", "Playbook file (required)")
	generateCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file (required)")
	generateCmd.Flags().StringVarP(&hostname, "hostname", "H", "", "Hostname (required)")
	generateCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file (required)")

	generateCmd.MarkFlagRequired("playbook")
	generateCmd.MarkFlagRequired("inventory")
	generateCmd.MarkFlagRequired("hostname")
	generateCmd.MarkFlagRequired("output")

	compileCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file (required)")

	compileCmd.MarkFlagRequired("inventory")
	compileCmd.MarkFlagRequired("hostname")
	compileCmd.MarkFlagRequired("output")

	RootCmd.AddCommand(runCmd)
	RootCmd.AddCommand(generateCmd)
	RootCmd.AddCommand(compileCmd)
}
