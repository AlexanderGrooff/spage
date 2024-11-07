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
	db            *database.DB
)

var rootCmd = &cobra.Command{
	Use:   "spage",
	Short: "Simple Playbook AGEnt",
	Long:  `A lightweight configuration management tool that compiles your playbooks into a single binary.`,
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the playbook",
	Run: func(cmd *cobra.Command, args []string) {
		if err := pkg.Execute(generated.Graph, inventoryFile); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a binary from a playbook",
	Run: func(cmd *cobra.Command, args []string) {
		gen := generator.NewGenerator(db)
		binaryPath, err := gen.GenerateBinary(playbookFile, outputFile)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Printf("Generated binary in %s\n", binaryPath)
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
	generateCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output binary name (required)")

	generateCmd.MarkFlagRequired("playbook")
	generateCmd.MarkFlagRequired("output")

	// Initialize database
	var err error
	db, err = database.NewDB("spage.db")
	if err != nil {
		fmt.Printf("Failed to initialize database: %s\n", err)
		os.Exit(1)
	}

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(webCmd)
}

func main() {

	if err := Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}

func Execute() error {
	return rootCmd.Execute()
}
