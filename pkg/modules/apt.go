package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"gopkg.in/yaml.v3"
)

// AptModule implements the Ansible 'apt' module logic.
type AptModule struct{}

func (am AptModule) InputType() reflect.Type {
	return reflect.TypeOf(&AptInput{})
}

func (am AptModule) OutputType() reflect.Type {
	return reflect.TypeOf(AptOutput{})
}

// AptInput defines the parameters for the apt module.
type AptInput struct {
	Name        interface{} `yaml:"name"`         // Name of the package(s) (string or list of strings)
	State       string      `yaml:"state"`        // present (default), absent, latest
	UpdateCache bool        `yaml:"update_cache"` // Run apt-get update before action
	// Internal storage for parsed package list
	PkgNames []string
	pkg.ModuleInput
}

// AptOutput defines the output of the apt module.
type AptOutput struct {
	Packages    []string `yaml:"packages"` // Packages operated on
	Versions    []string `yaml:"versions"` // Versions installed/removed (if applicable)
	State       string   `yaml:"state"`    // Final state (installed, removed, updated)
	WasChanged  bool     // Internal flag to track if apt made changes
	UpdateCache bool     // Whether cache update was performed
	pkg.ModuleOutput
}

// ToCode converts the AptInput struct into its Go code representation.
func (i *AptInput) ToCode() string {
	var nameCode string
	// Format Name field for code gen
	switch v := i.Name.(type) {
	case string:
		nameCode = fmt.Sprintf("%q", v)
	case []interface{}:
		var strElements []string
		for _, item := range v {
			if strItem, ok := item.(string); ok {
				strElements = append(strElements, fmt.Sprintf("%q", strItem))
			} else {
				strElements = append(strElements, "\"\"")
			}
		}
		// Need to generate the `[]interface{}{...}` representation here if Name is expected
		// to remain []interface{} in the generated struct, or handle conversion.
		// Let's assume Name field in generated struct can be []string directly if parsed from list.
		// If the generated struct's Name must be interface{}, this needs adjustment.
		nameCode = fmt.Sprintf("[]string{%s}", strings.Join(strElements, ", ")) // Assuming Name can be []string
	case []string: // Handle case where Name might already be []string (e.g. if ToCode is called later)
		var strElements []string
		for _, strItem := range v {
			strElements = append(strElements, fmt.Sprintf("%q", strItem))
		}
		nameCode = fmt.Sprintf("[]string{%s}", strings.Join(strElements, ", "))
	default:
		nameCode = "nil"
	}

	// Format PkgNames field for code gen (should be populated by Validate/parse)
	var pkgNamesCode string
	if len(i.PkgNames) > 0 {
		var pkgNameElements []string
		for _, pkgName := range i.PkgNames {
			pkgNameElements = append(pkgNameElements, fmt.Sprintf("%q", pkgName))
		}
		pkgNamesCode = fmt.Sprintf("[]string{%s}", strings.Join(pkgNameElements, ", "))
	} else {
		pkgNamesCode = "nil" // Generate nil if empty
	}

	// Return code for a pointer literal including PkgNames initialization
	return fmt.Sprintf("&modules.AptInput{Name: %s, State: %q, UpdateCache: %t, PkgNames: %s}",
		nameCode,
		i.State,
		i.UpdateCache,
		pkgNamesCode, // Add the generated code for PkgNames
	)
}

// GetVariableUsage identifies variables used in the apt parameters.
func (i *AptInput) GetVariableUsage() []string {
	vars := []string{}
	if nameStr, ok := i.Name.(string); ok {
		vars = append(vars, pkg.GetVariableUsageFromTemplate(nameStr)...)
	} else if nameList, ok := i.Name.([]interface{}); ok {
		for _, item := range nameList {
			if itemName, ok := item.(string); ok {
				vars = append(vars, pkg.GetVariableUsageFromTemplate(itemName)...)
			}
		}
	}
	// Also consider State potentially templated?
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.State)...)
	return vars
}

// parseAndValidatePackages extracts package names from Name field and validates them.
func (i *AptInput) parseAndValidatePackages() error {
	i.PkgNames = []string{}
	if i.Name == nil {
		// Allowed if update_cache is true
		if !i.UpdateCache {
			return fmt.Errorf("apt module requires 'name' or 'update_cache=true'")
		}
		return nil
	}

	switch v := i.Name.(type) {
	case string:
		if v == "" && !i.UpdateCache {
			return fmt.Errorf("apt module requires non-empty 'name' or 'update_cache=true'")
		}
		if v != "" {
			i.PkgNames = append(i.PkgNames, v)
		}
	case []interface{}:
		if len(v) == 0 && !i.UpdateCache {
			return fmt.Errorf("apt module requires non-empty 'name' list or 'update_cache=true'")
		}
		for idx, item := range v {
			if nameStr, ok := item.(string); ok {
				if nameStr == "" {
					return fmt.Errorf("package name at index %d cannot be empty", idx)
				}
				i.PkgNames = append(i.PkgNames, nameStr)
			} else {
				return fmt.Errorf("invalid type for package name at index %d: expected string, got %T", idx, item)
			}
		}
	default:
		return fmt.Errorf("invalid type for 'name' parameter: expected string or list of strings, got %T", i.Name)
	}

	// Check if PkgNames is empty when update_cache is false
	if len(i.PkgNames) == 0 && !i.UpdateCache {
		return fmt.Errorf("apt module requires at least one package name or 'update_cache=true'")
	}

	return nil
}

// Validate checks if the input parameters are valid.
func (i *AptInput) Validate() error {
	if err := i.parseAndValidatePackages(); err != nil {
		return err
	}

	if i.State == "" {
		i.State = "present" // Default state
	}
	switch i.State {
	case "present", "absent", "latest":
		// Valid states
	default:
		return fmt.Errorf("invalid state %q for apt module, must be one of: present, absent, latest", i.State)
	}
	return nil
}

// HasRevert indicates if the action can be reverted.
func (i *AptInput) HasRevert() bool {
	// Can revert install/remove if we have package names.
	// update_cache is not easily revertible.
	// The PkgNames field should be populated correctly either by UnmarshalYAML->parseAndValidatePackages
	// or by the code generated by ToCode.
	// _ = i.parseAndValidatePackages() // REMOVED: Redundant and caused issues by resetting PkgNames.
	return len(i.PkgNames) > 0 && (i.State == "present" || i.State == "absent")
}

// String provides a string representation of the AptOutput.
func (o AptOutput) String() string {
	parts := []string{}
	if len(o.Packages) > 0 {
		parts = append(parts, fmt.Sprintf("packages=[%s]", strings.Join(o.Packages, ", ")))
	}
	if len(o.Versions) > 0 {
		parts = append(parts, fmt.Sprintf("versions=[%s]", strings.Join(o.Versions, ", ")))
	}
	if o.State != "" {
		parts = append(parts, fmt.Sprintf("state=%s", o.State))
	}
	if o.UpdateCache {
		parts = append(parts, "update_cache=true")
	}
	return strings.Join(parts, ", ")
}

// Changed indicates if the apt module made changes to the system.
func (o AptOutput) Changed() bool {
	return o.WasChanged
}

// AsFacts implements the pkg.FactProvider interface.
func (o AptOutput) AsFacts() map[string]interface{} {
	facts := map[string]interface{}{}
	if len(o.Packages) > 0 {
		facts["packages"] = o.Packages // Keep as list
	}
	if len(o.Versions) > 0 {
		facts["versions"] = o.Versions
	}
	facts["state"] = o.State
	facts["changed"] = o.Changed()
	facts["update_cache"] = o.UpdateCache
	return facts
}

// runAptCommand executes an apt command, handling sudo and common options.
func runAptCommand(c *pkg.HostContext, runAsUser string, args ...string) (string, string, bool, error) {
	baseCmd := []string{
		"apt-get",
		"-q", // Non-interactive
		"-y", // Assume yes
		"--allow-downgrades",
		"--allow-remove-essential",
		"--allow-change-held-packages",
	}
	fullCmd := append(baseCmd, args...)
	cmdString := strings.Join(fullCmd, " ")

	// Use the provided runAsUser parameter
	stdout, stderr, err := c.RunCommand(cmdString, runAsUser)

	if err != nil {
		return stdout, stderr, false, fmt.Errorf("apt command failed: %w\nStderr: %s", err, stderr)
	}

	// Crude way to check if apt reported changes.
	// Look for lines indicating installation, removal, or upgrade.
	// A more robust method would parse apt output more carefully or check dpkg status.
	changed := strings.Contains(stdout, "The following packages will be REMOVED") ||
		strings.Contains(stdout, "The following NEW packages will be installed") ||
		strings.Contains(stdout, "The following packages will be upgraded") ||
		strings.Contains(stdout, "Updating registry") || // Consider cache update a change
		strings.Contains(stderr, "Updating registry") // Sometimes update logs to stderr?

	return stdout, stderr, changed, nil
}

// isPackageInstalled checks if a package is installed using dpkg-query.
func isPackageInstalled(c *pkg.HostContext, pkgName string) (bool, error) {
	// dpkg-query exits with 0 if installed, non-zero otherwise.
	// Use RunCommand without assuming root initially, as dpkg-query might not need it.
	// Need double backslash for the single quote in Go string literal
	cmdString := fmt.Sprintf("dpkg-query -W -f='${Status}' %s", pkgName)
	stdout, stderr, err := c.RunCommand(cmdString, "") // Run as default user first

	if err != nil {
		// If error contains "no packages found matching", it means not installed.
		if strings.Contains(stderr, "no packages found matching") || strings.Contains(stdout, "no packages found matching") {
			return false, nil
		} else if strings.Contains(err.Error(), "exit status 1") {
			// Other exit status 1 might also mean not installed
			return false, nil
		}
		// Any other error is unexpected.
		return false, fmt.Errorf("dpkg-query failed: %w\nStderr: %s", err, stderr)
	}

	// Check the output for standard installed statuses.
	return strings.Contains(stdout, "install ok installed") || strings.Contains(stdout, "install ok unpacked"), nil
}

// Execute runs the apt module logic.
func (m AptModule) Execute(params pkg.ModuleInput, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	i := params.(*AptInput)
	// Ensure PkgNames is populated (Validate should have been called by framework)
	if i.PkgNames == nil {
		if err := i.parseAndValidatePackages(); err != nil {
			// This path indicates Validate wasn't called or failed silently before Execute
			return nil, fmt.Errorf("internal state error: packages not parsed before Execute: %w", err)
		}
	}

	output := AptOutput{Packages: i.PkgNames, UpdateCache: i.UpdateCache}
	var overallChanged bool

	// --- Templating package names (already done in parseAndValidatePackages if needed) ---
	// We need to template *before* validation/parsing ideally.
	// For now, assume Validate is called *after* potential templating by the core engine.
	// Let's re-template here just in case, though it's inefficient.
	templatedPkgNames := []string{}
	for _, name := range i.PkgNames {
		templatedName, err := pkg.TemplateString(name, closure)
		if err != nil {
			return nil, fmt.Errorf("failed to template package name '%s': %w", name, err)
		}
		templatedPkgNames = append(templatedPkgNames, templatedName)
	}
	output.Packages = templatedPkgNames // Use templated names in output
	// --- End Templating ---

	if i.UpdateCache {
		common.LogDebug("Updating apt cache", map[string]interface{}{"host": closure.HostContext.Host.Name})
		_, _, cacheChanged, err := runAptCommand(closure.HostContext, runAs, "update")
		if err != nil {
			return nil, fmt.Errorf("failed to update apt cache: %w", err)
		}
		overallChanged = overallChanged || cacheChanged
	}

	if len(templatedPkgNames) == 0 {
		// Only update_cache was requested
		output.State = "cache_updated"
		output.WasChanged = overallChanged
		return output, nil
	}

	// --- Package State Logic (Handle List) ---
	pkgsToInstall := []string{}
	pkgsToRemove := []string{}
	finalState := i.State // Assume this unless something specific happens

	for _, pkgName := range templatedPkgNames {
		installed, err := isPackageInstalled(closure.HostContext, pkgName)
		if err != nil {
			return nil, fmt.Errorf("failed to check package status for %s: %w", pkgName, err)
		}

		switch i.State {
		case "present", "latest":
			if !installed || i.State == "latest" {
				pkgsToInstall = append(pkgsToInstall, pkgName)
				// If any package needs install/upgrade, the overall state is likely 'installed' or 'upgraded'
				// We'll refine state after the command runs
			} else {
				common.LogDebug("Package already present", map[string]interface{}{"host": closure.HostContext.Host.Name, "package": pkgName})
			}
		case "absent":
			if installed {
				pkgsToRemove = append(pkgsToRemove, pkgName)
			} else {
				common.LogDebug("Package already absent", map[string]interface{}{"host": closure.HostContext.Host.Name, "package": pkgName})
			}
		}
	}

	// --- Execute apt commands if needed ---
	if len(pkgsToInstall) > 0 {
		common.LogDebug("Ensuring packages are present/latest", map[string]interface{}{"host": closure.HostContext.Host.Name, "packages": pkgsToInstall, "state": i.State})
		args := append([]string{"install"}, pkgsToInstall...)
		_, _, changed, err := runAptCommand(closure.HostContext, runAs, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to install/upgrade packages %v: %w", pkgsToInstall, err)
		}
		overallChanged = overallChanged || changed
		finalState = "installed" // Simplification: assume installed/upgraded
	}

	if len(pkgsToRemove) > 0 {
		common.LogDebug("Ensuring packages are absent", map[string]interface{}{"host": closure.HostContext.Host.Name, "packages": pkgsToRemove})
		args := append([]string{"remove"}, pkgsToRemove...)
		_, _, changed, err := runAptCommand(closure.HostContext, runAs, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to remove packages %v: %w", pkgsToRemove, err)
		}
		overallChanged = overallChanged || changed
		finalState = "removed"
	}

	output.State = finalState
	output.WasChanged = overallChanged
	// TODO: Fetch installed versions for output.Versions if applicable
	return output, nil
}

// Revert attempts to undo the action performed by Execute.
func (m AptModule) Revert(params pkg.ModuleInput, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	i := params.(*AptInput)
	// Ensure PkgNames is populated
	if i.PkgNames == nil {
		if err := i.parseAndValidatePackages(); err != nil {
			// This path indicates Validate wasn't called or failed silently before Revert
			return nil, fmt.Errorf("internal state error: packages not parsed before Revert: %w", err)
		}
	}

	// --- Re-template package names ---
	templatedPkgNames := []string{}
	for _, name := range i.PkgNames {
		templatedName, err := pkg.TemplateString(name, closure)
		if err != nil {
			return nil, fmt.Errorf("failed to template package name '%s' for revert: %w", name, err)
		}
		templatedPkgNames = append(templatedPkgNames, templatedName)
	}
	// --- End Templating ---

	if len(templatedPkgNames) == 0 || !i.HasRevert() {
		return AptOutput{State: "norevert"}, nil // Cannot revert only cache update or latest state
	}

	var revertAction string
	var pkgsForRevertAction []string
	// Determine revert action based on the *original* intended state
	switch i.State {
	case "present":
		// If we intended present, revert is to remove
		revertAction = "remove"
		pkgsForRevertAction = templatedPkgNames
	case "absent":
		// If we intended absent, revert is to install
		revertAction = "install"
		pkgsForRevertAction = templatedPkgNames
	default:
		return AptOutput{State: "norevert", Packages: templatedPkgNames}, fmt.Errorf("cannot revert state %q for packages %v", i.State, templatedPkgNames)
	}

	if len(pkgsForRevertAction) > 0 {
		common.LogDebug("Reverting apt action", map[string]interface{}{"host": closure.HostContext.Host.Name, "packages": pkgsForRevertAction, "revert_action": revertAction})
		args := append([]string{revertAction}, pkgsForRevertAction...)
		_, _, _, err := runAptCommand(closure.HostContext, runAs, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to revert apt action (%s) for packages %v: %w", revertAction, pkgsForRevertAction, err)
		}
	}

	// Output state reflects the revert action attempt
	return AptOutput{Packages: templatedPkgNames, State: fmt.Sprintf("reverted_%s", revertAction), WasChanged: true}, nil
}

// UnmarshalYAML handles shorthand and list formats for the apt module 'name'.
func (i *AptInput) UnmarshalYAML(node *yaml.Node) error {
	// Default values
	i.State = "present"
	i.UpdateCache = false

	if node.Kind == yaml.ScalarNode && (node.Tag == "!!str" || node.Tag == "") {
		// Shorthand: apt: package_name (implies state: present)
		i.Name = node.Value
		return i.parseAndValidatePackages() // Parse/Validate immediately
	}

	if node.Kind == yaml.MappingNode {
		// Use a temporary type to avoid recursion
		type AptInputMap struct {
			Name        interface{} `yaml:"name"` // Accept string or list
			Pkg         interface{} `yaml:"pkg"`  // Alias, accept string or list
			State       string      `yaml:"state"`
			UpdateCache *bool       `yaml:"update_cache"` // Use pointer for explicit false
		}
		var tmp AptInputMap
		if err := node.Decode(&tmp); err != nil {
			return fmt.Errorf("failed to decode apt input map (line %d): %w", node.Line, err)
		}

		if tmp.Name != nil && tmp.Pkg != nil {
			return fmt.Errorf("cannot specify both 'name' and 'pkg' for apt module (line %d)", node.Line)
		}

		if tmp.Pkg != nil {
			i.Name = tmp.Pkg
		} else {
			i.Name = tmp.Name // Can be string, list, or nil
		}

		if tmp.State != "" {
			i.State = tmp.State
		}
		if tmp.UpdateCache != nil {
			i.UpdateCache = *tmp.UpdateCache
		}

		return i.parseAndValidatePackages() // Parse/Validate name field
	}

	return fmt.Errorf("invalid type for apt module input (line %d): expected string or map, got %s", node.Line, node.Tag)
}

func init() {
	pkg.RegisterModule("apt", AptModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m AptModule) ParameterAliases() map[string]string {
	return map[string]string{
		"pkg": "name",
	}
}
