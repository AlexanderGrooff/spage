package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/daemon"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/spf13/cobra"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/config"
	_ "github.com/AlexanderGrooff/spage/pkg/modules" // Register modules
)

var (
	playbookFile   string
	outputFile     string
	inventoryFile  string
	configFile     string
	tags           []string
	skipTags       []string
	cfg            *config.Config // Store the loaded config
	checkMode      bool
	diffMode       bool
	extraVars      []string
	becomeMode     bool
	limitHosts     string // Host pattern to limit execution to
	connectionType string // Connection type override (e.g., local)
	verbose        bool

	// Daemon communication flags
	daemonGRPC string
	playID     string

	// Bundle create flags
	bundleDir        string
	bundleRootPath   string
	bundlePlaybookID string
	apiHTTPBase      string
	bundleOutput     string
	bundleFilePath   string
	// CLI auth / enrollment
	apiToken string
)

// LoadConfig loads the configuration and applies settings
var LoadConfig = func(configFile string) error {
	configPaths := []string{}
	if configFile == "" {
		defaultConfig := "spage.yaml"
		if _, err := os.Stat(defaultConfig); err == nil {
			configPaths = append(configPaths, defaultConfig)
		}
	} else {
		configPaths = append(configPaths, configFile)
	}

	var err error
	cfg, err = config.Load(configPaths...)
	if err != nil {
		if configFile != "" || !os.IsNotExist(err) {
			return fmt.Errorf("failed to load configuration from %v: %w", configPaths, err)
		}
		// If no specific config file found/specified, try loading defaults
		cfg, err = config.Load() // Load defaults
		if err != nil {
			return fmt.Errorf("failed to load default configuration: %w", err)
		}
	}

	// Apply logging configuration AFTER loading config
	common.SetLogLevel(cfg.Logging.Level)
	if cfg.Logging.File != "" {
		if err := common.SetLogFile(cfg.Logging.File); err != nil {
			return fmt.Errorf("error setting log file: %w", err)
		}
	}
	// Call SetLogFormat with the loaded logging config
	if err := common.SetLogFormat(cfg.Logging); err != nil {
		return fmt.Errorf("error setting log format: %w", err)
	}

	if verbose {
		common.SetLogLevel("debug")
	}

	return nil
}

var RootCmd = &cobra.Command{
	Use:   "spage",
	Short: "Simple Playbook AGEnt",
	Long:  `A lightweight configuration management tool that compiles your playbooks into a Go program to run on a host.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := LoadConfig(configFile); err != nil {
			return err
		}
		// auto-fill api token from config once config is loaded
		if apiToken == "" && cfg != nil && cfg.ApiToken != "" {
			apiToken = cfg.ApiToken
		}
		return nil
	},
}

func GetGraph(playbookFile string, tags, skipTags []string, baseConfig *config.Config, becomeMode bool) (pkg.Graph, error) {
	// Override config with command line flags if provided
	if len(tags) > 0 {
		baseConfig.Tags.Tags = tags
	}
	if len(skipTags) > 0 {
		baseConfig.Tags.SkipTags = skipTags
	}

	// Expect a local path here. Bundle resolution is handled by the caller (run/generate commands)

	graph, err := pkg.NewGraphFromFile(playbookFile, baseConfig.RolesPath)
	if err != nil {
		return pkg.Graph{}, fmt.Errorf("failed to generate graph from playbook: %w", err)
	}

	// Apply become mode if enabled
	if becomeMode {
		applyBecomeToGraph(&graph)
	}

	// Apply tag filtering to the graph
	filteredGraph, err := applyTagFiltering(graph, baseConfig.Tags)
	if err != nil {
		return pkg.Graph{}, fmt.Errorf("failed to apply tag filtering: %w", err)
	}
	return filteredGraph, nil
}

// materializeBundleToFS downloads a bundle archive and loads it into an in-memory fs.FS.
// Returns the fs, the root playbook path within the FS, and a cleanup func (no-op).
func materializeBundleToFS(bundleRef string) (fs.FS, string, func(), error) {
	const prefix = "spage://bundle/"
	ref := strings.TrimPrefix(bundleRef, prefix)
	parts := strings.SplitN(ref, "#", 2)
	if len(parts) != 2 {
		return nil, "", nil, fmt.Errorf("invalid bundle ref: %s", bundleRef)
	}
	bundleID := parts[0]
	rootPath := parts[1]

	url := fmt.Sprintf("%s/api/bundles/%s/archive", strings.TrimRight(cfg.API.HTTPBase, "/"), bundleID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, "", nil, err
	}
	if apiToken == "" && cfg != nil && cfg.ApiToken != "" {
		apiToken = cfg.ApiToken
	}
	if apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+apiToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to download bundle archive %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", nil, fmt.Errorf("bundle archive %s http %d", url, resp.StatusCode)
	}
	memfs, err := pkg.NewMemFSFromTarGz(resp.Body)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to load bundle into FS: %w", err)
	}
	cleanup := func() {}
	return memfs, filepath.ToSlash(rootPath), cleanup, nil
}

// createTarGzFromDir writes a tar.gz of srcDir into w, preserving relative paths
func createTarGzFromDir(srcDir string, w io.Writer) error {
	gw := gzip.NewWriter(w)
	defer func() { _ = gw.Close() }()
	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	srcDir = filepath.Clean(srcDir)
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, ".git/") {
			return nil
		}
		hdr := &tar.Header{
			Name:    rel,
			Mode:    int64(info.Mode().Perm()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		return nil
	})
}

func GetDaemonClient() (*daemon.Client, error) {
	// Initialize daemon client if daemon communication is enabled
	var daemonClient *daemon.Client
	var err error

	// Check if daemon communication is enabled via config or CLI flags
	daemonEnabled := cfg.Daemon.Enabled || daemonGRPC != "" || playID != ""

	if daemonEnabled {
		// Determine daemon endpoint (CLI flag takes precedence over config)
		daemonEndpoint := daemonGRPC
		if daemonEndpoint == "" {
			daemonEndpoint = cfg.Daemon.Endpoint
		}
		if daemonEndpoint == "" {
			daemonEndpoint = "localhost:9091"
		}

		// Determine play ID (CLI flag takes precedence over config)
		playIDToUse := playID
		if playIDToUse == "" {
			playIDToUse = cfg.Daemon.PlayID
		}
		if playIDToUse == "" {
			generatedTaskID := uuid.New().String()
			common.LogInfo("No play ID provided, generating a new one", map[string]interface{}{
				"play_id": generatedTaskID,
			})
			playIDToUse = generatedTaskID
		}

		daemonClient, err = daemon.NewClient(&daemon.Config{
			Endpoint: daemonEndpoint,
			PlayID:   playIDToUse,
			Timeout:  cfg.Daemon.Timeout,
		})
		if err != nil {
			common.LogError("Failed to create daemon client", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}
	}

	return daemonClient, nil
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a graph from a playbook and save it as Go code",
	Args:  cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Accept playbook as positional argument (preferred), fallback to --playbook flag
		if playbookFile == "" {
			if len(args) >= 1 {
				playbookFile = args[0]
			}
		}
		if playbookFile == "" {
			return fmt.Errorf("playbook file is required; provide it as a positional argument (spage run playbook.yaml) or with --playbook")
		}

		// Resolve bundle references using FS (no disk extraction)
		var cleanup func()
		var graph pkg.Graph
		var err error
		if strings.HasPrefix(playbookFile, "spage://bundle/") {
			fsys, rootPath, c, err := materializeBundleToFS(playbookFile)
			if err != nil {
				common.LogError("Failed to load bundle FS", map[string]interface{}{"error": err.Error()})
				os.Exit(1)
			}
			cleanup = c
			pkg.SetSourceFSForCLI(fsys)
			defer pkg.ClearSourceFSForCLI()
			if cleanup != nil {
				defer cleanup()
			}
			graph, err = pkg.NewGraphFromFS(fsys, rootPath, cfg.RolesPath)
			if err != nil {
				return fmt.Errorf("failed to generate graph: %w", err)
			}
		} else {
			graph, err = GetGraph(playbookFile, tags, skipTags, cfg, becomeMode)
		}
		if err != nil {
			common.LogError("Failed to generate graph", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}

		err = graph.SaveToFile(outputFile)
		common.LogInfo("Compiled binary", map[string]interface{}{
			"output_file": outputFile,
		})
		if err != nil {
			common.LogError("Failed to generate graph", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}
		return nil
	},
}

var runCmd = &cobra.Command{
	Use:   "run [playbook]",
	Short: "Run a playbook by compiling & executing it",
	Args:  cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Accept playbook as positional argument (preferred), fallback to --playbook flag
		if playbookFile == "" {
			if len(args) >= 1 {
				playbookFile = args[0]
			}
		}
		if playbookFile == "" {
			return fmt.Errorf("playbook file is required; provide it as a positional argument (spage run playbook.yaml) or with --playbook")
		}
		daemonClient, err := GetDaemonClient()
		if err != nil {
			return fmt.Errorf("failed to get daemon client: %w", err)
		}

		// Resolve bundle references using FS (no disk extraction)
		var cleanup func()
		var graph pkg.Graph
		if strings.HasPrefix(playbookFile, "spage://bundle/") {
			fsys, rootPath, c, err := materializeBundleToFS(playbookFile)
			if err != nil {
				if daemonClient != nil {
					_ = pkg.ReportPlayError(daemonClient, err)
				}
				return fmt.Errorf("failed to load bundle FS: %w", err)
			}
			cleanup = c
			pkg.SetSourceFSForCLI(fsys)
			defer pkg.ClearSourceFSForCLI()
			if cleanup != nil {
				defer cleanup()
			}
			graph, err = pkg.NewGraphFromFS(fsys, rootPath, cfg.RolesPath)
			if err != nil {
				if daemonClient != nil {
					_ = pkg.ReportPlayError(daemonClient, err)
				}
				return fmt.Errorf("failed to generate graph: %w", err)
			}
		} else {
			var err error
			graph, err = GetGraph(playbookFile, tags, skipTags, cfg, becomeMode)
			if err != nil {
				if daemonClient != nil {
					_ = pkg.ReportPlayError(daemonClient, err)
				}
				return fmt.Errorf("failed to generate graph: %w", err)
			}
		}

		// Determine effective limit: command-line flag takes precedence over config
		effectiveLimit := limitHosts
		if effectiveLimit == "" && cfg.Limit != "" {
			effectiveLimit = cfg.Limit
		}

		// Execute with host limiting handled in the executor layer
		if effectiveLimit != "" {
			common.LogInfo("Limiting hosts for execution", map[string]interface{}{
				"limit": effectiveLimit,
			})
		}

		// Apply connection override (CLI takes precedence)
		if connectionType != "" {
			cfg.Connection = connectionType
		}

		if checkMode {
			if cfg.Facts == nil {
				cfg.Facts = make(map[string]interface{})
			}
			cfg.Facts["ansible_check_mode"] = true
		}
		if diffMode {
			if cfg.Facts == nil {
				cfg.Facts = make(map[string]interface{})
			}
			cfg.Facts["ansible_diff"] = true
		}

		// Parse and merge extra variables
		if len(extraVars) > 0 {
			if cfg.Facts == nil {
				cfg.Facts = make(map[string]interface{})
			}
			extraFacts, err := parseExtraVars(extraVars)
			if err != nil {
				return fmt.Errorf("failed to parse extra variables: %w", err)
			}
			// Merge extra facts into cfg.Facts (extra vars take precedence)
			maps.Copy(cfg.Facts, extraFacts)

		}

		// Close daemon client on exit
		defer func() {
			if daemonClient != nil {
				// Close the daemon client
				if err := daemonClient.Close(); err != nil {
					common.LogWarn("Failed to close daemon client", map[string]interface{}{
						"error": err.Error(),
					})
				}
			}
		}()

		if cfg.Executor == "temporal" {
			err = StartTemporalExecutorWithLimit(&graph, inventoryFile, cfg, daemonClient, effectiveLimit)
		} else {
			err = StartLocalExecutorWithLimit(&graph, inventoryFile, cfg, daemonClient, effectiveLimit)
		}

		if err != nil {
			if daemonClient != nil {
				if err := pkg.ReportPlayError(daemonClient, err); err != nil {
					common.LogWarn("failed to report play error", map[string]interface{}{"error": err.Error()})
				}
			}
			common.LogError("Failed to run playbook", map[string]interface{}{
				"error": err.Error(),
			})
		}

		// Wait for all daemon reports to complete before proceeding
		if daemonClient != nil {
			// First wait for all pending reports to be sent
			if err := pkg.WaitForPendingReportsWithTimeout(30 * time.Second); err != nil {
				common.LogWarn("Timeout waiting for daemon reports", map[string]interface{}{
					"error": err.Error(),
				})
			}

			// Then wait for streams to finish processing any pending data
			if err := daemonClient.WaitForStreamsToFinish(5 * time.Second); err != nil {
				common.LogWarn("Timeout waiting for streams to finish", map[string]interface{}{
					"error": err.Error(),
				})
			}
		}

		return nil
	},
}

// inventory parent command
var inventoryCmd = &cobra.Command{
	Use:   "inventory",
	Short: "Inventory-related commands",
}

// inventory list subcommand
var inventoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all inventory hosts and groups in JSON format",
	Long: `List all inventory hosts and groups in JSON format, similar to ansible-inventory --list.
This command supports both static inventory files and dynamic inventory plugins.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		err := LoadConfig(configFile)

		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if inventoryFile == "" {
			inventoryFile = cfg.Inventory
		}
		if inventoryFile == "" {
			return fmt.Errorf("inventory file not specified and no default inventory configured")
		}
		common.LogDebug("Loading inventory", map[string]interface{}{
			"inventory_file": inventoryFile,
		})
		// Use our inventory loading system which supports plugins
		inventory, err := pkg.LoadInventoryWithPaths(inventoryFile, cfg.Inventory, ".", "", cfg)
		if err != nil {
			return fmt.Errorf("failed to load inventory: %w", err)
		}

		// Convert to ansible-inventory --list format
		output, err := formatInventoryAsJSON(inventory)
		if err != nil {
			return fmt.Errorf("failed to format inventory: %w", err)
		}

		fmt.Println(output)
		return nil
	},
}

// bundle parent and create subcommand
var bundleCmd = &cobra.Command{
	Use:   "bundle",
	Short: "Bundle-related commands",
}

var bundleCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a playbook bundle tar.gz on disk",
	RunE: func(cmd *cobra.Command, args []string) error {
		if bundleDir == "" {
			return fmt.Errorf("--dir is required")
		}
		// Derive default output if not set
		out := bundleOutput
		if out == "" {
			base := filepath.Base(filepath.Clean(bundleDir))
			if base == "." || base == "" {
				base = "bundle"
			}
			out = fmt.Sprintf("%s-bundle.tar.gz", base)
		}
		f, err := os.Create(out)
		if err != nil {
			return fmt.Errorf("failed to create output: %w", err)
		}
		if err := createTarGzFromDir(bundleDir, f); err != nil {
			_ = f.Close()
			_ = os.Remove(out)
			return fmt.Errorf("failed to create tar.gz: %w", err)
		}
		_ = f.Close()
		info, _ := os.Stat(out)
		if bundleRootPath != "" {
			fmt.Printf("Bundle created at %s (root path: %s, size: %d bytes)\n", out, bundleRootPath, func() int64 {
				if info != nil {
					return info.Size()
				}
				return 0
			}())
		} else {
			fmt.Printf("Bundle created at %s (size: %d bytes)\n", out, func() int64 {
				if info != nil {
					return info.Size()
				}
				return 0
			}())
		}
		return nil
	},
}

var bundleUploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "Upload a bundle tar.gz to the API",
	RunE: func(cmd *cobra.Command, args []string) error {
		if bundleFilePath == "" || bundleRootPath == "" {
			return fmt.Errorf("--file and --root are required")
		}
		// Determine API base
		base := ""
		if cfg != nil && cfg.API.HTTPBase != "" {
			base = cfg.API.HTTPBase
		}
		if apiHTTPBase != "" {
			base = apiHTTPBase
		}
		// Resolve token: config or env
		token := ""
		if cfg != nil && cfg.ApiToken != "" {
			token = cfg.ApiToken
		}
		if token == "" {
			token = os.Getenv("SPAGE_API_TOKEN")
		}
		if token == "" {
			return fmt.Errorf("no API token found (set spage_api_token in config or SPAGE_API_TOKEN env)")
		}

		// Open bundle file
		f, err := os.Open(bundleFilePath)
		if err != nil {
			return fmt.Errorf("failed to open bundle file: %w", err)
		}
		defer func() { _ = f.Close() }()

		// Prepare multipart
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, err := mw.CreateFormFile("file", filepath.Base(bundleFilePath))
		if err != nil {
			return fmt.Errorf("failed to create form file: %w", err)
		}
		if _, err := io.Copy(fw, f); err != nil {
			return fmt.Errorf("failed to write file part: %w", err)
		}
		if err := mw.WriteField("root_path", bundleRootPath); err != nil {
			return fmt.Errorf("failed to write root_path: %w", err)
		}
		if bundlePlaybookID != "" {
			if err := mw.WriteField("playbook_id", bundlePlaybookID); err != nil {
				return fmt.Errorf("failed to write playbook_id: %w", err)
			}
		}
		if err := mw.Close(); err != nil {
			return fmt.Errorf("failed to finalize form: %w", err)
		}

		// POST to API
		url := fmt.Sprintf("%s/api/bundles", strings.TrimRight(base, "/"))
		req, err := http.NewRequest(http.MethodPost, url, &body)
		if err != nil {
			return fmt.Errorf("failed to build request: %w", err)
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("upload failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("upload failed: http %d: %s", resp.StatusCode, string(b))
		}
		var out map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return fmt.Errorf("invalid response: %w", err)
		}
		fmt.Printf("Bundle uploaded: id=%v content_sha256=%v\n", out["id"], out["content_sha256"])
		return nil
	},
}

func init() {
	// Add config flag to root command so it's available to all subcommands
	RootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "Config file path (default: ./spage.yaml)")
	RootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging (sets log level to debug)")

	generateCmd.Flags().StringVarP(&playbookFile, "playbook", "p", "", "Playbook file (required)")
	generateCmd.Flags().StringVarP(&outputFile, "output", "o", "generated_tasks.go", "Output file (default: generated_tasks.go)")
	generateCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file (default: localhost)")
	generateCmd.Flags().StringVarP(&limitHosts, "limit", "l", "", "Limit selected hosts to an additional pattern")
	generateCmd.Flags().StringSliceVarP(&tags, "tags", "t", []string{}, "Only include tasks with these tags (comma-separated)")
	generateCmd.Flags().StringSliceVar(&skipTags, "skip-tags", []string{}, "Skip tasks with these tags (comma-separated)")
	generateCmd.Flags().BoolVar(&becomeMode, "become", false, "Run all tasks with become: true and become_user: root")

	runCmd.Flags().StringVarP(&playbookFile, "playbook", "p", "", "Playbook file")
	runCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file (default: localhost)")
	runCmd.Flags().StringVarP(&outputFile, "output", "o", "generated_tasks.go", "Output file (default: generated_tasks.go)")
	runCmd.Flags().StringVarP(&limitHosts, "limit", "l", "", "Limit selected hosts to an additional pattern")
	runCmd.Flags().StringSliceVarP(&tags, "tags", "t", []string{}, "Only include tasks with these tags (comma-separated)")
	runCmd.Flags().StringSliceVar(&skipTags, "skip-tags", []string{}, "Skip tasks with these tags (comma-separated)")
	runCmd.Flags().BoolVar(&checkMode, "check", false, "Enable check mode (dry run)")
	runCmd.Flags().BoolVar(&diffMode, "diff", false, "Enable diff mode")
	runCmd.Flags().StringSliceVarP(&extraVars, "extra-vars", "e", []string{}, "Set additional variables as key=value or YAML/JSON, i.e. -e 'key1=value1' -e 'key2=value2' or -e '{\"key1\": \"value1\", \"key2\": \"value2\"}'")
	runCmd.Flags().BoolVar(&becomeMode, "become", false, "Run all tasks with become: true and become_user: root")
	runCmd.Flags().StringVar(&connectionType, "connection", "", "Connection type override (e.g., local)")

	// Daemon communication flags
	runCmd.Flags().StringVar(&daemonGRPC, "daemon-grpc", "", "Daemon gRPC endpoint (default: localhost:9091)")
	runCmd.Flags().StringVar(&playID, "play-id", "", "Play ID for daemon communication")

	// Inventory list flags
	inventoryListCmd.Flags().StringVarP(&inventoryFile, "inventory", "i", "", "Inventory file or directory")

	// Add inventory subcommands
	inventoryCmd.AddCommand(inventoryListCmd)
	RootCmd.AddCommand(inventoryCmd)

	// Bundle create flags
	bundleCreateCmd.Flags().StringVarP(&bundleDir, "dir", "d", "", "Directory to bundle (required)")
	bundleCreateCmd.Flags().StringVarP(&bundleOutput, "output", "o", "", "Output tar.gz path (default: <dir_basename>-bundle.tar.gz)")
	_ = bundleCreateCmd.MarkFlagRequired("dir")

	// Bundle upload flags
	bundleUploadCmd.Flags().StringVarP(&bundleFilePath, "file", "f", "", "Bundle tar.gz file to upload (required)")
	bundleUploadCmd.Flags().StringVarP(&bundleRootPath, "root", "r", "", "Root playbook path inside the bundle (required)")
	bundleUploadCmd.Flags().StringVar(&bundlePlaybookID, "playbook-id", "", "Optional playbook ID to link the bundle to")
	bundleUploadCmd.Flags().StringVar(&apiHTTPBase, "api", "", "API HTTP base (default http://localhost:1323)")
	_ = bundleUploadCmd.MarkFlagRequired("file")
	_ = bundleUploadCmd.MarkFlagRequired("root")

	bundleCmd.AddCommand(bundleCreateCmd)
	bundleCmd.AddCommand(bundleUploadCmd)

	RootCmd.AddCommand(generateCmd)
	RootCmd.AddCommand(runCmd)
	RootCmd.AddCommand(bundleCmd)
	RootCmd.AddCommand(inventoryCmd)
}

// GetConfig returns the loaded configuration
func GetConfig() *config.Config {
	return cfg
}

// applyBecomeToGraph applies become: true and become_user: root to all tasks in the graph
func applyBecomeToGraph(graph *pkg.Graph) {
	for i := range graph.Tasks {
		for j := range graph.Tasks[i] {
			graph.Tasks[i][j].Become = true
			graph.Tasks[i][j].BecomeUser = "root"
		}
	}
}

// applyTagFiltering filters tasks based on tag configuration
func applyTagFiltering(graph pkg.Graph, tagsConfig config.TagsConfig) (pkg.Graph, error) {
	// Always apply filtering to handle special tags like "never" correctly
	// Even when no specific tags are requested, "never" tagged tasks should be excluded

	filteredGraph := pkg.Graph{
		RequiredInputs: graph.RequiredInputs, // Copy required inputs
		Tasks:          make([][]pkg.Task, 0),
		Handlers:       graph.Handlers, // Copy handlers
		Vars:           graph.Vars,
		PlaybookPath:   graph.PlaybookPath, // Copy base path
	}

	for _, taskLayer := range graph.Tasks {
		var filteredLayer []pkg.Task
		for _, task := range taskLayer {
			if shouldIncludeTask(task, tagsConfig) {
				filteredLayer = append(filteredLayer, task)
			}
		}
		// Only add the layer if it has tasks
		if len(filteredLayer) > 0 {
			filteredGraph.Tasks = append(filteredGraph.Tasks, filteredLayer)
		}
	}

	return filteredGraph, nil
}

// shouldIncludeTask determines if a task should be included based on its tags
func shouldIncludeTask(task pkg.Task, tagsConfig config.TagsConfig) bool {
	taskTags := task.Tags

	// Special tag handling: "always" tag means always include (unless skipped)
	hasAlwaysTag := contains(taskTags, "always")

	// Special tag handling: "never" tag means never include (unless explicitly tagged)
	hasNeverTag := contains(taskTags, "never")

	// Check if task should be skipped
	if len(tagsConfig.SkipTags) > 0 {
		for _, skipTag := range tagsConfig.SkipTags {
			if contains(taskTags, skipTag) {
				return false // Skip this task
			}
		}
	}

	// Handle "never" tag: only include if explicitly requested
	if hasNeverTag {
		if len(tagsConfig.Tags) == 0 {
			// No specific tags requested, exclude "never" tasks
			return false
		}
		// Check if "never" tag is explicitly requested
		hasMatchingTag := false
		for _, wantedTag := range tagsConfig.Tags {
			if contains(taskTags, wantedTag) {
				hasMatchingTag = true
				break
			}
		}
		if !hasMatchingTag {
			return false
		}
	}

	// If task has "always" tag, include it (unless skipped above)
	if hasAlwaysTag {
		return true
	}

	// If no specific tags requested, include all tasks (except "never" which was handled above)
	if len(tagsConfig.Tags) == 0 {
		return true
	}

	// Check if task has any of the requested tags
	for _, wantedTag := range tagsConfig.Tags {
		if contains(taskTags, wantedTag) {
			return true
		}
	}

	// If task has no tags but we're filtering by tags, exclude it
	if len(taskTags) == 0 {
		return false
	}

	// Task doesn't match any wanted tags
	return false
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// parseExtraVars parses extra variables from command line arguments.
// Supports both key=value format and JSON/YAML format.
func parseExtraVars(extraVars []string) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for _, extraVar := range extraVars {
		// Check if it's a key=value format
		if strings.Contains(extraVar, "=") {
			parts := strings.SplitN(extraVar, "=", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid key=value format: %s", extraVar)
			}
			key := parts[0]
			value := parts[1]

			// Try to parse as JSON first, then as string
			var parsedValue interface{}
			if err := json.Unmarshal([]byte(value), &parsedValue); err == nil {
				result[key] = parsedValue
			} else {
				// If not valid JSON, treat as string
				result[key] = value
			}
		} else {
			// Try to parse as JSON/YAML object
			var parsedMap map[string]interface{}
			if err := json.Unmarshal([]byte(extraVar), &parsedMap); err == nil {
				// Merge the parsed map into result
				for k, v := range parsedMap {
					result[k] = v
				}
			} else {
				// Try YAML parsing
				if err := yaml.Unmarshal([]byte(extraVar), &parsedMap); err == nil {
					// Merge the parsed map into result
					for k, v := range parsedMap {
						result[k] = v
					}
				} else {
					return nil, fmt.Errorf("invalid extra variable format: %s (expected key=value or JSON/YAML object)", extraVar)
				}
			}
		}
	}

	return result, nil
}

// formatInventoryAsJSON converts our inventory format to ansible-inventory --list JSON format
func formatInventoryAsJSON(inventory *pkg.Inventory) (string, error) {
	// Create the output structure similar to ansible-inventory --list
	output := make(map[string]interface{})

	// Add _meta with hostvars
	meta := map[string]interface{}{
		"hostvars": make(map[string]interface{}),
	}

	// Process hosts and collect their variables
	for hostName, host := range inventory.Hosts {
		if host.Vars != nil {
			meta["hostvars"].(map[string]interface{})[hostName] = host.Vars
		}
	}
	output["_meta"] = meta

	// Add groups
	for groupName, group := range inventory.Groups {
		groupData := make(map[string]interface{})

		// Add hosts to group
		if len(group.Hosts) > 0 {
			hostList := make([]string, 0, len(group.Hosts))
			for hostName := range group.Hosts {
				hostList = append(hostList, hostName)
			}
			groupData["hosts"] = hostList
		} else {
			groupData["hosts"] = []string{}
		}

		// Add group variables
		if group.Vars != nil {
			groupData["vars"] = group.Vars
		}

		output[groupName] = groupData
	}

	// Add all group containing all hosts (Ansible convention)
	allHosts := make([]string, 0, len(inventory.Hosts))
	for hostName := range inventory.Hosts {
		allHosts = append(allHosts, hostName)
	}

	// If 'all' group doesn't exist, create it
	if _, exists := output["all"]; !exists {
		allGroup := map[string]interface{}{
			"hosts": allHosts,
		}
		if inventory.Vars != nil {
			allGroup["vars"] = inventory.Vars
		}
		output["all"] = allGroup
	}

	// Convert to JSON
	jsonBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal inventory to JSON: %w", err)
	}

	return string(jsonBytes), nil
}
