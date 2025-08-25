package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AlexanderGrooff/spage/cmd"
	"github.com/AlexanderGrooff/spage/pkg"
)

func main() {
	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start command execution in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- cmd.RootCmd.Execute()
	}()

	// Wait for either command completion or signal
	select {
	case err := <-errChan:
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case sig := <-sigChan:
		fmt.Fprintf(os.Stderr, "Received signal %v, shutting down gracefully...\n", sig)
		// Wait for any pending daemon reports to complete
		_ = pkg.WaitForPendingReportsWithTimeout(10 * time.Second)
		os.Exit(0)
	}
}
