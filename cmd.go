//go:generate go run generate_tasks.go -file playbook.yaml

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AlexanderGrooff/spage/pkg"
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

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a graph from a playbook and save it as Go code",
	Run: func(cmd *cobra.Command, args []string) {
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
		compiledGraph.SaveToFile(outputFile)
		fmt.Printf("Compiled binary in %s\n", outputFile)
	},
}

func init() {
	generateCmd.Flags().StringVarP(&playbookFile, "playbook", "p", "", "Playbook file (required)")
	generateCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file (required)")
	generateCmd.Flags().StringVarP(&hostname, "hostname", "H", "", "Hostname (required)")
	generateCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file (required)")

	generateCmd.MarkFlagRequired("playbook")
	generateCmd.MarkFlagRequired("inventory")
	generateCmd.MarkFlagRequired("hostname")
	generateCmd.MarkFlagRequired("output")

	RootCmd.AddCommand(generateCmd)
}
