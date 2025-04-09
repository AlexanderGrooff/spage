package main

import (
	"fmt"
	"os"

	"github.com/AlexanderGrooff/spage/cmd"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
