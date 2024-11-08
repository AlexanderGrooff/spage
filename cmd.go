//go:generate go run generate_tasks.go -file playbook.yaml

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AlexanderGrooff/spage/generated"
	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/database"
	"github.com/AlexanderGrooff/spage/pkg/generator"
	"github.com/AlexanderGrooff/spage/pkg/web"
)

var (
	playbookFile  string
	outputFile    string
	inventoryFile string
	hostname      string
	db            *database.DB
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
		gen := generator.NewGenerator(db)
		binaryPath, err := gen.GenerateGraphFromPlaybook(playbookFile, ".")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Printf("Saved generated graph in %s\n", binaryPath)
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
		gen := generator.NewGenerator(db)
		binaryPath, err := gen.BuildBinaryFromGraphForHost(&compiledGraph, outputFile, inventoryFile, hostname)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Printf("Compiled binary in %s\n", binaryPath)
	},
}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Run the web server",
	Run: func(cmd *cobra.Command, args []string) {
		server := web.NewServer(db)
		server.Start()
	},
}

func init() {
	runCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file (required)")
	generateCmd.Flags().StringVarP(&playbookFile, "playbook", "p", "", "Playbook file (required)")

	generateCmd.MarkFlagRequired("playbook")

	compileCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file (required)")
	compileCmd.Flags().StringVarP(&hostname, "hostname", "", "", "Hostname (required)")

	compileCmd.MarkFlagRequired("inventory")
	compileCmd.MarkFlagRequired("hostname")

	// Initialize database
	var err error
	db, err = database.NewDB("spage.db")
	if err != nil {
		fmt.Printf("Failed to initialize database: %s\n", err)
		os.Exit(1)
	}

	RootCmd.AddCommand(runCmd)
	RootCmd.AddCommand(generateCmd)
	RootCmd.AddCommand(webCmd)
	RootCmd.AddCommand(compileCmd)
}
