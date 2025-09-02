package executor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
)

func InitializeRecapStats(hostContexts map[string]*pkg.HostContext) map[string]map[string]int {
	recapStats := make(map[string]map[string]int)
	for hostname := range hostContexts {
		recapStats[hostname] = map[string]int{"ok": 0, "changed": 0, "failed": 0, "skipped": 0, "ignored": 0}
	}
	return recapStats
}

func CalculateExpectedResults(
	nodesInLevel []pkg.GraphNode,
	hostContexts map[string]*pkg.HostContext,
	cfg *config.Config,
) (int, error) {
	numExpectedResultsOnLevel := 0
	for _, node := range nodesInLevel {
		task, ok := node.(*pkg.Task)
		if !ok {
			// Not a task so it won't produce any results
			continue
		}

		// Special handling for run_once, as it executes on one host but produces results for all.
		// TODO: template the run_once condition with actual closure
		if !task.RunOnce.IsEmpty() && task.RunOnce.IsTruthy(nil) {
			if len(hostContexts) > 0 {
				// One execution (or one aggregated execution for loops) produces a result for every host.
				numExpectedResultsOnLevel += len(hostContexts)
			}
			continue
		}

		// For regular tasks, count executions across all hosts.
		for _, hc := range hostContexts {
			// Create a temporary closure for delegate_to resolution.
			// The actual host context used for closure calculation depends on delegate_to.
			resolutionClosure := task.ConstructClosure(hc, cfg)
			effectiveHostCtx, err := GetDelegatedHostContext(*task, hostContexts, resolutionClosure, cfg)
			if err != nil {
				return 0, fmt.Errorf("failed to resolve delegate_to for count on task '%s', host '%s': %w", task.Name, hc.Host.Name, err)
			}
			if effectiveHostCtx == nil {
				effectiveHostCtx = hc // No delegation, use original host context.
			}

			closures, err := GetTaskClosures(*task, effectiveHostCtx, cfg)
			if err != nil {
				return 0, fmt.Errorf("failed to get task closures for count on task '%s', host '%s': %w", task.Name, effectiveHostCtx.Host.Name, err)
			}
			numExpectedResultsOnLevel += len(closures)
		}
	}
	return numExpectedResultsOnLevel, nil
}

func PrepareLevelHistoryAndGetCount(
	nodesInLevel []pkg.GraphNode,
	hostContexts map[string]*pkg.HostContext,
	executionLevel int,
	cfg *config.Config,
) (map[string]chan pkg.GraphNode, int, error) {
	levelHistoryForRevert := make(map[string]chan pkg.GraphNode)
	for hostname := range hostContexts {
		// Buffer size can be a best-effort guess; it doesn't need to be perfect for local execution.
		// A safer approach might be to calculate per-host task count, but total count is simpler.
		levelHistoryForRevert[hostname] = make(chan pkg.GraphNode, len(nodesInLevel)*2) // Simple heuristic
	}

	numExpectedResultsOnLevel, err := CalculateExpectedResults(nodesInLevel, hostContexts, cfg)
	if err != nil {
		return nil, 0, err
	}

	return levelHistoryForRevert, numExpectedResultsOnLevel, nil
}

// GetFirstAvailableHost returns the first host from the hostContexts map
// Used for run_once tasks to select the execution host. It sorts the hosts
// by name to ensure deterministic execution.
func GetFirstAvailableHost(hostContexts map[string]*pkg.HostContext) (*pkg.HostContext, string) {
	if len(hostContexts) == 0 {
		return nil, ""
	}

	// Get host names and sort them to ensure consistent ordering
	hostNames := make([]string, 0, len(hostContexts))
	for hostName := range hostContexts {
		hostNames = append(hostNames, hostName)
	}
	sort.Strings(hostNames)

	// Return the first host context based on sorted order
	firstName := hostNames[0]
	return hostContexts[firstName], firstName
}

// GetTaskClosures generates one or more Closures for a task, handling loops.
func GetTaskClosures(task pkg.Task, c *pkg.HostContext, cfg *config.Config) ([]*pkg.Closure, error) {
	if task.Loop == nil {
		closure := task.ConstructClosure(c, cfg)
		return []*pkg.Closure{closure}, nil
	}

	loopItems, err := ParseLoop(task, c, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse loop for task '%s' on host '%s': %w", task.Name, c.Host.Name, err)
	}

	var closures []*pkg.Closure
	for _, item := range loopItems {
		tClosure := task.ConstructClosure(c, cfg)
		// "item" is the standard variable name for loop items.
		// Ansible's loop_control.loop_var allows customization; this is not currently supported here.
		tClosure.ExtraFacts["item"] = item
		closures = append(closures, tClosure)
	}
	return closures, nil
}

// ParseLoop parses the loop directive of a task, evaluating expressions and returning items to iterate over.
func ParseLoop(task pkg.Task, c *pkg.HostContext, cfg *config.Config) ([]interface{}, error) {
	var loopItems []interface{}
	closure := task.ConstructClosure(c, cfg)

	switch loopValue := task.Loop.(type) {
	case string:
		trimmedLoopStr := strings.TrimSpace(loopValue)
		// Check if it looks like a simple variable template {{ var }}
		if strings.HasPrefix(trimmedLoopStr, "{{") && strings.HasSuffix(trimmedLoopStr, "}}") {
			varName := strings.TrimSpace(trimmedLoopStr[2 : len(trimmedLoopStr)-2])
			// Attempt to directly look up the variable in the facts
			factValue, found := c.Facts.Load(varName) // Use Facts.Load
			if found {
				val := reflect.ValueOf(factValue)
				if val.Kind() == reflect.Slice {
					loopItems = make([]interface{}, val.Len())
					for i := 0; i < val.Len(); i++ {
						loopItems[i] = val.Index(i).Interface()
					}
					// else: not a slice, fall through to string templating logic below
				}
				// else: not found, fall through to string templating logic below
			}
		}

		// If loopItems is still nil, proceed with templating the string.
		if loopItems == nil {
			evalLoopStr, err := pkg.TemplateString(loopValue, closure)
			if err != nil {
				return nil, fmt.Errorf("failed to template loop string '%s' for task '%s': %w", loopValue, task.Name, err)
			}
			loopItems = []interface{}{}
			if evalLoopStr != "" { // Avoid creating [""] for empty strings
				for _, itemStr := range strings.Split(evalLoopStr, "\n") {
					loopItems = append(loopItems, itemStr)
				}
			}
		}

	case []interface{}:
		loopItems = loopValue

	default:
		return nil, fmt.Errorf("unsupported loop type '%T' for task '%s'", task.Loop, task.Name)
	}
	return loopItems, nil
}

func GetDelegatedHostContext(task pkg.Task, hostContexts map[string]*pkg.HostContext, closure *pkg.Closure, cfg *config.Config) (*pkg.HostContext, error) {
	if task.DelegateTo != "" {
		// Template the delegate_to value in case it contains Jinja variables
		delegateTo, err := pkg.TemplateString(task.DelegateTo, closure)
		if err != nil {
			return nil, fmt.Errorf("failed to template delegate_to '%s': %w", task.DelegateTo, err)
		}

		// First try to find the host in the existing hostContexts
		hostContext, ok := hostContexts[delegateTo]
		if ok {
			return hostContext, nil
		}

		// If not found in inventory, check for special/implicit hosts
		if delegateTo == "localhost" {
			// Create a localhost host dynamically
			localhostHost := &pkg.Host{
				Name:    "localhost",
				Host:    "localhost",
				IsLocal: true,
				Vars:    make(map[string]interface{}),
				Groups:  make(map[string]string),
			}

			// Initialize host context for localhost
			localhostContext, err := pkg.InitializeHostContext(localhostHost, cfg)
			if err != nil {
				return nil, fmt.Errorf("failed to initialize context for localhost: %w", err)
			}

			// Copy all facts from the original closure's context to the localhost context
			// This ensures that variables set in previous tasks are available
			closure.HostContext.Facts.Range(func(key, value interface{}) bool {
				localhostContext.Facts.Store(key, value)
				return true
			})

			// Add basic localhost facts (override any existing ones)
			localhostContext.Facts.Store("inventory_hostname", "localhost")
			localhostContext.Facts.Store("ansible_hostname", "localhost")

			return localhostContext, nil
		}

		// Could extend this to handle other special hosts in the future
		// For example: 127.0.0.1, or dynamic IP addresses

		return nil, fmt.Errorf("host context for delegate_to '%s' not found", delegateTo)
	}
	return nil, nil
}

// CreateRunOnceResultsForAllHosts creates TaskResult copies for all hosts when run_once is used
// This ensures that facts and results are propagated to all hosts in the batch
func CreateRunOnceResultsForAllHosts(originalResult pkg.TaskResult, hostContexts map[string]*pkg.HostContext, executionHostName string) []pkg.TaskResult {
	var results []pkg.TaskResult

	for _, hostCtx := range hostContexts {
		// Create a copy of the result for each host
		result := pkg.TaskResult{
			Task:                    originalResult.Task,
			Error:                   originalResult.Error,
			Status:                  originalResult.Status,
			Failed:                  originalResult.Failed,
			Changed:                 originalResult.Changed,
			Duration:                originalResult.Duration,
			Output:                  originalResult.Output,
			ExecutionSpecificOutput: originalResult.ExecutionSpecificOutput,
		}

		// Create a new closure for each host with their own context
		result.Closure = &pkg.Closure{
			HostContext: hostCtx,
			ExtraFacts:  make(map[string]interface{}),
		}

		// Copy extra facts from the original closure
		if originalResult.Closure != nil {
			for k, v := range originalResult.Closure.ExtraFacts {
				result.Closure.ExtraFacts[k] = v
			}
		}

		// For non-execution hosts, mark as skipped to reflect run_once semantics
		if hostCtx.Host != nil && hostCtx.Host.Name != executionHostName {
			result.Status = pkg.TaskStatusSkipped
			result.Changed = false
			result.Failed = false
			// Preserve Error only for execution host
			result.Error = nil
		}

		results = append(results, result)
	}

	return results
}

// PPrintOutput prints the output of a module or an error in a readable format.
func PPrintOutput(output pkg.ModuleOutput, err error) {
	if output != nil {
		// Attempt to get a string representation. Assumes ModuleOutput implements fmt.Stringer or can be printed directly.
		// More sophisticated printing might involve checking specific fields or using a custom Marshal-like method.
		fmt.Printf("%v\n", output)
		// else if err != nil: Error is usually printed by the caller context, so no duplicate print here.
	}
}

// ResultChannel abstracts over different channel types used by local and temporal executors
type ResultChannel interface {
	ReceiveResult() (pkg.TaskResult, bool, error)
	IsClosed() bool
}

// ErrorChannel abstracts over different error channel types
type ErrorChannel interface {
	ReceiveError() (error, bool, error)
	IsClosed() bool
}

// Logger abstracts over different logging implementations
type Logger interface {
	Error(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Debug(msg string, args ...interface{})
}

// LocalResultChannel implements ResultChannel for standard Go channels
type LocalResultChannel struct {
	ch chan pkg.TaskResult
}

func NewLocalResultChannel(ch chan pkg.TaskResult) *LocalResultChannel {
	return &LocalResultChannel{ch: ch}
}

func (c *LocalResultChannel) ReceiveResult() (pkg.TaskResult, bool, error) {
	result, ok := <-c.ch
	return result, ok, nil
}

func (c *LocalResultChannel) IsClosed() bool {
	// For local channels, we can't easily check if closed without receiving
	// This will be handled by the receive loop
	return false
}

// LocalErrorChannel implements ErrorChannel for standard Go channels
type LocalErrorChannel struct {
	ch chan error
}

func NewLocalErrorChannel(ch chan error) *LocalErrorChannel {
	return &LocalErrorChannel{ch: ch}
}

func (c *LocalErrorChannel) ReceiveError() (error, bool, error) {
	// Non-blocking receive to prevent deadlocks in the processing loop.
	select {
	case err, ok := <-c.ch:
		return err, ok, nil
	default:
		return nil, true, nil // Channel is open, but no error is present.
	}
}

func (c *LocalErrorChannel) IsClosed() bool {
	return false
}

// ContextAwareResultChannel wraps a LocalResultChannel to handle context cancellation
type ContextAwareResultChannel struct {
	ctx context.Context
	ch  *LocalResultChannel
}

func NewContextAwareResultChannel(ctx context.Context, ch *LocalResultChannel) *ContextAwareResultChannel {
	return &ContextAwareResultChannel{ctx: ctx, ch: ch}
}

func (c *ContextAwareResultChannel) ReceiveResult() (pkg.TaskResult, bool, error) {
	select {
	case <-c.ctx.Done():
		return pkg.TaskResult{}, false, c.ctx.Err()
	default:
		return c.ch.ReceiveResult()
	}
}

func (c *ContextAwareResultChannel) IsClosed() bool {
	select {
	case <-c.ctx.Done():
		return true
	default:
		return c.ch.IsClosed()
	}
}

// ContextAwareErrorChannel wraps a LocalErrorChannel to handle context cancellation
type ContextAwareErrorChannel struct {
	ctx context.Context
	ch  *LocalErrorChannel
}

func NewContextAwareErrorChannel(ctx context.Context, ch *LocalErrorChannel) *ContextAwareErrorChannel {
	return &ContextAwareErrorChannel{ctx: ctx, ch: ch}
}

func (c *ContextAwareErrorChannel) ReceiveError() (error, bool, error) {
	select {
	case <-c.ctx.Done():
		return c.ctx.Err(), true, nil
	default:
		return c.ch.ReceiveError()
	}
}

func (c *ContextAwareErrorChannel) IsClosed() bool {
	select {
	case <-c.ctx.Done():
		return true
	default:
		return c.ch.IsClosed()
	}
}

// LocalLogger implements Logger for standard logging
type LocalLogger struct{}

func NewLocalLogger() *LocalLogger {
	return &LocalLogger{}
}

func (l *LocalLogger) Error(msg string, args ...interface{}) {
	data := make(map[string]interface{})
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			if key, ok := args[i].(string); ok {
				data[key] = args[i+1]
			}
		}
	}
	common.LogError(msg, data)
}

func (l *LocalLogger) Warn(msg string, args ...interface{}) {
	data := make(map[string]interface{})
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			if key, ok := args[i].(string); ok {
				data[key] = args[i+1]
			}
		}
	}
	common.LogWarn(msg, data)
}

func (l *LocalLogger) Info(msg string, args ...interface{}) {
	data := make(map[string]interface{})
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			if key, ok := args[i].(string); ok {
				data[key] = args[i+1]
			}
		}
	}
	common.LogInfo(msg, data)
}

func (l *LocalLogger) Debug(msg string, args ...interface{}) {
	data := make(map[string]interface{})
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			if key, ok := args[i].(string); ok {
				data[key] = args[i+1]
			}
		}
	}
	common.LogDebug(msg, data)
}

// ResultProcessor handles the common logic for processing individual task results
type ResultProcessor struct {
	ExecutionLevel int
	Logger         Logger
	Config         *config.Config
}

// ProcessTaskResult handles the common logic for processing a single task result
// Returns whether the result represents a hard error that should stop execution
func (rp *ResultProcessor) ProcessTaskResult(
	result pkg.TaskResult,
	recapStats map[string]map[string]int,
	executionHistoryLevel map[string]chan pkg.GraphNode,
) bool {
	return rp.processResult(result, recapStats, executionHistoryLevel, "TASK", true)
}

// ProcessHandlerResult handles the common logic for processing a single handler result
// Returns whether the result represents a hard error (always false for handlers - they continue on error)
func (rp *ResultProcessor) ProcessHandlerResult(
	result pkg.TaskResult,
	recapStats map[string]map[string]int,
) bool {
	return rp.processResult(result, recapStats, nil, "HANDLER TASK", false)
}

// processResult handles the common logic for processing both task and handler results
// taskType should be "TASK" or "HANDLER TASK"
// stopOnError determines if errors should halt execution (true for tasks, false for handlers)
func (rp *ResultProcessor) processResult(
	result pkg.TaskResult,
	recapStats map[string]map[string]int,
	executionHistoryLevel map[string]chan pkg.GraphNode,
	taskType string,
	stopOnError bool,
) bool {
	if result.Closure == nil || result.Closure.HostContext == nil || result.Closure.HostContext.Host == nil {
		rp.Logger.Error("Received TaskResult with nil Closure/HostContext/Host",
			"level", rp.ExecutionLevel, "result_task_name", result.Task.Name, "task_type", taskType)
		return stopOnError
	}

	hostname := result.Closure.HostContext.Host.Name
	task := result.Task

	// Ensure recapStats entry exists for this hostname (e.g., for delegate_to localhost)
	if _, exists := recapStats[hostname]; !exists {
		recapStats[hostname] = map[string]int{"ok": 0, "changed": 0, "failed": 0, "skipped": 0, "ignored": 0}
	}

	// Handle task history recording for local executor (only for regular tasks)
	if executionHistoryLevel != nil {
		if hostChan, ok := executionHistoryLevel[hostname]; ok {
			select {
			case hostChan <- &task:
			default:
				rp.Logger.Warn("Failed to record task in history channel (full or closed)",
					"task", task.Name, "host", hostname)
			}
		}
	}

	// Store task output in history
	if result.Closure.HostContext.History != nil {
		result.Closure.HostContext.History.Store(task.Name, result.Output)
	}

	// Print task header for plain format
	if rp.Config.Logging.Format == "plain" {
		fmt.Printf("\n%s [%s] (%s) ****************************************************\n", taskType, task.Name, hostname)
	}

	logData := map[string]interface{}{
		"host":      hostname,
		"task":      task.Name,
		"task_type": taskType,
		"duration":  result.Duration.String(),
		"status":    result.Status.String(),
	}

	var ignoredErrWrapper *pkg.IgnoredTaskError
	isIgnoredError := errors.As(result.Error, &ignoredErrWrapper)
	isHardError := false

	if isIgnoredError {
		originalErr := ignoredErrWrapper.Unwrap()
		logData["ignored"] = true
		logData["error_original"] = originalErr.Error()
		if result.Output != nil {
			logData["output"] = result.Output.String()
		}
		recapStats[hostname]["ignored"]++
		if result.Failed {
			recapStats[hostname]["failed"]++
		}

		if rp.Config.Logging.Format == "plain" {
			fmt.Printf("ignored: [%s] => %v\n", hostname, originalErr)
			PPrintOutput(result.Output, originalErr)
		} else {
			rp.Logger.Warn(taskType+" failed (ignored)", logData)
		}
	} else if result.Error != nil {
		isHardError = stopOnError // Only consider it a hard error if we should stop on error
		logData["error"] = result.Error.Error()
		if result.Output != nil {
			logData["output"] = result.Output.String()
		}
		if result.Failed {
			recapStats[hostname]["failed"]++
		}

		if rp.Config.Logging.Format == "plain" {
			fmt.Printf("failed: [%s] => (%v)\n", hostname, result.Error)
			PPrintOutput(result.Output, result.Error)
		} else {
			rp.Logger.Error(taskType+" failed", logData)
		}
	} else {
		switch result.Status {
		case pkg.TaskStatusChanged:
			logData["changed"] = true
			if result.Output != nil {
				logData["output"] = result.Output.String()
			}
			recapStats[hostname]["changed"]++
			if rp.Config.Logging.Format == "plain" {
				fmt.Printf("changed: [%s] => \n%v\n", hostname, result.Output)
			} else {
				rp.Logger.Info(taskType+" changed", logData)
			}
		case pkg.TaskStatusSkipped:
			recapStats[hostname]["skipped"]++
			if rp.Config.Logging.Format == "plain" {
				fmt.Printf("skipped: [%s]\n", hostname)
			} else {
				rp.Logger.Info(taskType+" skipped", logData)
			}
		default:
			logData["changed"] = false
			if result.Output != nil {
				logData["output"] = result.Output.String()
			}
			recapStats[hostname]["ok"]++
			if rp.Config.Logging.Format == "plain" {
				fmt.Printf("ok: [%s]\n", hostname)
				PPrintOutput(result.Output, nil)
			} else {
				rp.Logger.Info(taskType+" ok", logData)
			}
		}
	}

	return isHardError
}

// SharedProcessLevelResults contains the common logic for processing level results
// that can be used by both local and temporal executors
func SharedProcessLevelResults(
	resultsCh ResultChannel,
	errCh ErrorChannel,
	logger Logger,
	executionLevel int,
	cfg *config.Config,
	numExpectedResultsOnLevel int,
	recapStats map[string]map[string]int,
	executionHistoryLevel map[string]chan pkg.GraphNode, // nil for temporal executor
	onResult func(pkg.TaskResult) error, // additional processing for temporal executor
) (bool, []pkg.TaskResult, error) {
	var levelHardErrored bool
	resultsReceived := 0
	var dispatchError error
	var processedTasksOnLevel []pkg.TaskResult

	processor := &ResultProcessor{
		ExecutionLevel: executionLevel,
		Logger:         logger,
		Config:         cfg,
	}

	// Process results until we get all expected results or encounter an error
	for resultsReceived < numExpectedResultsOnLevel {
		// Try to receive from error channel first (non-blocking)
		if !errCh.IsClosed() {
			if err, ok, receiveErr := errCh.ReceiveError(); receiveErr == nil && ok && err != nil {
				dispatchError = err
				logger.Error("Dispatch error received from loadLevelTasks", "level", executionLevel, "error", dispatchError)
				levelHardErrored = true
				break
			}
		}

		// Try to receive from results channel
		result, ok, receiveErr := resultsCh.ReceiveResult()
		if receiveErr != nil {
			return true, processedTasksOnLevel, fmt.Errorf("error receiving result: %w", receiveErr)
		}

		if !ok {
			// Channel closed. Before declaring an error, do one last non-blocking check on the error channel
			// to catch a race condition where the dispatching goroutine errored out and closed the results channel.
			if dispatchError, ok, _ := errCh.ReceiveError(); ok && dispatchError != nil {
				return true, processedTasksOnLevel, fmt.Errorf("dispatch error occurred: %w", dispatchError)
			}

			// Channel closed
			if resultsReceived < numExpectedResultsOnLevel {
				return true, processedTasksOnLevel, fmt.Errorf("results channel closed prematurely on level %d. Expected %d, got %d", executionLevel, numExpectedResultsOnLevel, resultsReceived)
			}
			break
		}

		resultsReceived++
		processedTasksOnLevel = append(processedTasksOnLevel, result)

		// Additional processing for temporal executor (fact registration)
		if onResult != nil {
			if err := onResult(result); err != nil {
				logger.Error("Task processing reported an error after fact registration",
					"task", result.Task.Name, "host", result.Closure.HostContext.Host.Name, "error", err)
				levelHardErrored = true
			}
		}

		// Process the individual result
		if processor.ProcessTaskResult(result, recapStats, executionHistoryLevel) {
			levelHardErrored = true

			// In sequential mode, mark as failed but continue collecting results from other hosts
			// This ensures that revert operations can be triggered for all hosts
			if cfg.ExecutionMode == "sequential" && levelHardErrored {
				logger.Warn("Sequential mode: task hard-failed, but continuing to collect results from other hosts for proper revert execution.",
					"task", result.Task.Name, "host", result.Closure.HostContext.Host.Name, "level", executionLevel)
				// Don't break here - continue collecting results from other hosts
			}
		}
	}

	if dispatchError != nil {
		logger.Error("Level processing stopped due to error.", "level", executionLevel, "error", dispatchError, "results_received", resultsReceived)
		return true, processedTasksOnLevel, dispatchError
	}

	if resultsReceived < numExpectedResultsOnLevel {
		errMsg := fmt.Errorf("level %d did not receive all expected results: got %d, expected %d", executionLevel, resultsReceived, numExpectedResultsOnLevel)
		logger.Error(errMsg.Error())
		return true, processedTasksOnLevel, errMsg
	}

	return levelHardErrored, processedTasksOnLevel, nil
}

// BuildRegisteredFromOutput converts a ModuleOutput into a map suitable for `register:`
// and ensures consistent fields like "failed" and "changed" are present.
func BuildRegisteredFromOutput(output pkg.ModuleOutput, changed bool) interface{} {
	if output == nil {
		return map[string]interface{}{
			"failed":  false,
			"changed": changed,
		}
	}

	if v, ok := pkg.ConvertOutputToFactsMap(output).(map[string]interface{}); ok {
		// Ensure consistent fields
		v["failed"] = false
		if _, present := v["changed"]; !present {
			v["changed"] = changed
		}
		return v
	}

	// Fallback minimal map
	return map[string]interface{}{
		"failed":  false,
		"changed": changed,
	}
}

// PropagateRegisteredToAllHosts stores the provided registered value into all host contexts,
// and also mirrors it into the workflow-wide facts map if provided (Temporal executor path).
func PropagateRegisteredToAllHosts(
	task pkg.Task,
	registeredValue interface{},
	hostContexts map[string]*pkg.HostContext,
	workflowHostFacts map[string]map[string]interface{},
) {
	if task.Register == "" || registeredValue == nil {
		return
	}

	common.LogDebug("Propagating run_once facts to all hosts", map[string]interface{}{
		"task":     task.Name,
		"variable": task.Register,
	})

	for hostName, hc := range hostContexts {
		if hc != nil && hc.Facts != nil {
			hc.Facts.Store(task.Register, registeredValue)
		}
		if workflowHostFacts != nil {
			if _, ok := workflowHostFacts[hostName]; !ok {
				workflowHostFacts[hostName] = make(map[string]interface{})
			}
			workflowHostFacts[hostName][task.Register] = registeredValue
		}
	}
}
