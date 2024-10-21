package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var inventoryFile string

var rootCmd = &cobra.Command{
	Use:   "reconcile [file]",
	Short: "Reconcile the given file",
	Long:  `Reconcile the given file using the specified inventory file.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		file := args[0]
		if err := reconcile(file, inventoryFile); err != nil {
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
