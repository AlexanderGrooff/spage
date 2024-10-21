//go:generate go run generate_tasks.go -file playbook.yaml

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AlexanderGrooff/reconcile/generated"
	"github.com/AlexanderGrooff/reconcile/pkg"
)

// Task struct definition...

func main() {

	if err := Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}

var inventoryFile string

var rootCmd = &cobra.Command{
	Use:   "reconcile [file]",
	Short: "Reconcile the given file",
	Long:  `Reconcile the given file using the specified inventory file.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := pkg.Reconcile(generated.GeneratedTasks, inventoryFile); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file (required)")
	// rootCmd.MarkFlagRequired("inventory")
}
