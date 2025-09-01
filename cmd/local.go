package cmd

import (
	"fmt"
	"maps"

	"github.com/spf13/cobra"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/AlexanderGrooff/spage/pkg/executor"
)

func StartLocalExecutor(graph *pkg.Graph, inventoryFile string, cfg *config.Config, daemonClient interface{}) error {
	return StartLocalExecutorWithLimit(graph, inventoryFile, cfg, daemonClient, "")
}

func StartLocalExecutorWithLimit(graph *pkg.Graph, inventoryFile string, cfg *config.Config, daemonClient interface{}, limitPattern string) error {
	exec := executor.NewLocalGraphExecutor(&executor.LocalTaskRunner{})
	err := pkg.ExecuteGraphWithLimit(exec, graph, inventoryFile, cfg, daemonClient, limitPattern)
	if err != nil {
		fmt.Printf("Execution failed: %v\n", err)
		return err
	}
	return nil
}

var (
	localConfigFile    string
	localInventoryFile string
	localCheckMode     bool
	localDiffMode      bool
	localTags          []string
	localSkipTags      []string
	localExtraVars     []string
	localBecomeMode    bool
	localVerbose       bool
)

func NewLocalExecutorCmd(graph pkg.Graph) *cobra.Command {
	localCmd := &cobra.Command{
		Use:          "spage-playbook",
		Short:        "A pre-compiled Spage playbook that runs locally by default",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := LoadConfig(localConfigFile)
			if err != nil {
				fmt.Printf("Error loading config: %v\n", err)
				return err
			}

			// Set verbose logging if enabled
			if localVerbose {
				common.SetLogLevel("debug")
			}

			cfg := GetConfig()
			if localCheckMode {
				if cfg.Facts == nil {
					cfg.Facts = make(map[string]interface{})
				}
				cfg.Facts["ansible_check_mode"] = true
			}
			if localDiffMode {
				if cfg.Facts == nil {
					cfg.Facts = make(map[string]interface{})
				}
				cfg.Facts["ansible_diff"] = true
			}

			// Parse and merge extra variables
			if len(localExtraVars) > 0 {
				if cfg.Facts == nil {
					cfg.Facts = make(map[string]interface{})
				}
				extraFacts, err := parseExtraVars(localExtraVars)
				if err != nil {
					return fmt.Errorf("failed to parse extra variables: %w", err)
				}
				// Merge extra facts into cfg.Facts (extra vars take precedence)
				maps.Copy(cfg.Facts, extraFacts)
			}

			// Apply become mode if enabled
			if localBecomeMode {
				applyBecomeToGraph(&graph)
			}

			// Apply tag filtering to the graph
			if len(localTags) > 0 {
				cfg.Tags.Tags = localTags
			}
			if len(localSkipTags) > 0 {
				cfg.Tags.SkipTags = localSkipTags
			}

			filteredGraph, err := applyTagFiltering(graph, cfg.Tags)
			if err != nil {
				return fmt.Errorf("failed to apply tag filtering: %w", err)
			}

			return StartLocalExecutorWithLimit(&filteredGraph, localInventoryFile, cfg, nil, limitHosts)
		},
	}

	localCmd.Flags().StringVarP(&localConfigFile, "config", "c", "", "Config file path (default: ./spage.yaml)")
	localCmd.Flags().StringVarP(&localInventoryFile, "inventory", "i", "", "Inventory file path")
	localCmd.Flags().BoolVar(&localCheckMode, "check", false, "Enable check mode (dry run)")
	localCmd.Flags().BoolVar(&localDiffMode, "diff", false, "Enable diff mode")
	localCmd.Flags().StringSliceVarP(&localTags, "tags", "t", []string{}, "Only include tasks with these tags (comma-separated)")
	localCmd.Flags().StringSliceVar(&localSkipTags, "skip-tags", []string{}, "Skip tasks with these tags (comma-separated)")
	localCmd.Flags().StringSliceVarP(&localExtraVars, "extra-vars", "e", []string{}, "Set additional variables as key=value or YAML/JSON")
	localCmd.Flags().BoolVar(&localBecomeMode, "become", false, "Run all tasks with become: true and become_user: root")
	localCmd.Flags().BoolVarP(&localVerbose, "verbose", "v", false, "Enable verbose logging (sets log level to debug)")
	localCmd.Flags().StringVarP(&limitHosts, "limit", "l", "", "Limit execution to hosts matching the given pattern")

	return localCmd
}
