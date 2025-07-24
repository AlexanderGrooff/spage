package modules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
	"gopkg.in/yaml.v3"
)

// YumModule implements the Ansible 'yum' module logic.
type YumModule struct{}

func (ym YumModule) InputType() reflect.Type {
	return reflect.TypeOf(&YumInput{})
}

func (ym YumModule) OutputType() reflect.Type {
	return reflect.TypeOf(YumOutput{})
}

// YumInput defines the parameters for the yum module.
type YumInput struct {
	Name        interface{} `yaml:"name"`         // Name of the package(s) (string or list of strings)
	State       string      `yaml:"state"`        // present (default), absent, latest, installed, removed
	UpdateCache bool        `yaml:"update_cache"` // Run yum update before action
	Enablerepo  interface{} `yaml:"enablerepo"`   // List of repos to enable (string, comma-delimited string, or list)
	Disablerepo interface{} `yaml:"disablerepo"`  // List of repos to disable (string, comma-delimited string, or list)
	Exclude     interface{} `yaml:"exclude"`      // List of packages to exclude (string, comma-delimited string, or list)
	UpdateOnly  bool        `yaml:"update_only"`  // Only update packages, don't install/remove
	// Internal storage for parsed package list
	PkgNames []string
	// Internal storage for parsed repo lists
	EnablerepoList  []string
	DisablerepoList []string
	// Internal storage for parsed exclude list
	ExcludeList []string
}

// YumOutput defines the output of the yum module.
type YumOutput struct {
	Packages    []string `yaml:"packages"` // Packages operated on
	Versions    []string `yaml:"versions"` // Versions installed/removed (if applicable)
	State       string   `yaml:"state"`    // Final state (installed, removed, updated)
	WasChanged  bool     // Internal flag to track if yum made changes
	UpdateCache bool     // Whether cache update was performed
	pkg.ModuleOutput
}

// ToCode converts the YumInput struct into its Go code representation.
func (i *YumInput) ToCode() string {
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
		nameCode = fmt.Sprintf("[]string{%s}", strings.Join(strElements, ", "))
	case []string:
		var strElements []string
		for _, strItem := range v {
			strElements = append(strElements, fmt.Sprintf("%q", strItem))
		}
		nameCode = fmt.Sprintf("[]string{%s}", strings.Join(strElements, ", "))
	default:
		nameCode = "nil"
	}

	// Format PkgNames field for code gen
	var pkgNamesCode string
	if len(i.PkgNames) > 0 {
		var pkgNameElements []string
		for _, pkgName := range i.PkgNames {
			pkgNameElements = append(pkgNameElements, fmt.Sprintf("%q", pkgName))
		}
		pkgNamesCode = fmt.Sprintf("[]string{%s}", strings.Join(pkgNameElements, ", "))
	} else {
		pkgNamesCode = "nil"
	}

	// Format Enablerepo field
	var enablerepoCode string
	if len(i.EnablerepoList) > 0 {
		var repoElements []string
		for _, repo := range i.EnablerepoList {
			repoElements = append(repoElements, fmt.Sprintf("%q", repo))
		}
		enablerepoCode = fmt.Sprintf("[]string{%s}", strings.Join(repoElements, ", "))
	} else {
		enablerepoCode = "nil"
	}

	// Format Disablerepo field
	var disablerepoCode string
	if len(i.DisablerepoList) > 0 {
		var repoElements []string
		for _, repo := range i.DisablerepoList {
			repoElements = append(repoElements, fmt.Sprintf("%q", repo))
		}
		disablerepoCode = fmt.Sprintf("[]string{%s}", strings.Join(repoElements, ", "))
	} else {
		disablerepoCode = "nil"
	}

	// Format Exclude field
	var excludeCode string
	if len(i.ExcludeList) > 0 {
		var excludeElements []string
		for _, pkgName := range i.ExcludeList {
			excludeElements = append(excludeElements, fmt.Sprintf("%q", pkgName))
		}
		excludeCode = fmt.Sprintf("[]string{%s}", strings.Join(excludeElements, ", "))
	} else {
		excludeCode = "nil"
	}

	return fmt.Sprintf("&modules.YumInput{Name: %s, State: %q, UpdateCache: %t, Enablerepo: %s, Disablerepo: %s, Exclude: %s, UpdateOnly: %t, PkgNames: %s}",
		nameCode,
		i.State,
		i.UpdateCache,
		enablerepoCode,
		disablerepoCode,
		excludeCode,
		i.UpdateOnly,
		pkgNamesCode,
	)
}

// GetVariableUsage identifies variables used in the yum parameters.
func (i *YumInput) GetVariableUsage() []string {
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
	vars = append(vars, pkg.GetVariableUsageFromTemplate(i.State)...)
	for _, repo := range i.EnablerepoList {
		vars = append(vars, pkg.GetVariableUsageFromTemplate(repo)...)
	}
	for _, repo := range i.DisablerepoList {
		vars = append(vars, pkg.GetVariableUsageFromTemplate(repo)...)
	}
	for _, pkgName := range i.ExcludeList {
		vars = append(vars, pkg.GetVariableUsageFromTemplate(pkgName)...)
	}
	return vars
}

func (i *YumInput) ProvidesVariables() []string {
	return nil
}

// parseAndValidatePackages extracts package names from Name field and validates them.
func (i *YumInput) parseAndValidatePackages() error {
	i.PkgNames = []string{}
	if i.Name == nil {
		// Allowed if update_cache is true
		if !i.UpdateCache {
			return fmt.Errorf("yum module requires 'name' or 'update_cache=true'")
		}
		return nil
	}

	switch v := i.Name.(type) {
	case string:
		if v == "" && !i.UpdateCache {
			return fmt.Errorf("yum module requires non-empty 'name' or 'update_cache=true'")
		}
		if v != "" {
			i.PkgNames = append(i.PkgNames, v)
		}
	case []interface{}:
		if len(v) == 0 && !i.UpdateCache {
			return fmt.Errorf("yum module requires non-empty 'name' list or 'update_cache=true'")
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
	case []string:
		if len(v) == 0 && !i.UpdateCache {
			return fmt.Errorf("yum module requires non-empty 'name' list or 'update_cache=true'")
		}
		for idx, nameStr := range v {
			if nameStr == "" {
				return fmt.Errorf("package name at index %d cannot be empty", idx)
			}
			i.PkgNames = append(i.PkgNames, nameStr)
		}
	default:
		return fmt.Errorf("invalid type for 'name' parameter: expected string or list of strings, got %T", i.Name)
	}

	// Check if PkgNames is empty when update_cache is false
	if len(i.PkgNames) == 0 && !i.UpdateCache {
		return fmt.Errorf("yum module requires at least one package name or 'update_cache=true'")
	}

	return nil
}

// parseAndValidateAll extracts repository names and exclude packages from Enablerepo/Disablerepo/Exclude fields and validates them.
func (i *YumInput) parseAndValidateAll() error {
	i.EnablerepoList = []string{}
	i.DisablerepoList = []string{}
	i.ExcludeList = []string{}

	// Parse Enablerepo
	if i.Enablerepo != nil {
		switch v := i.Enablerepo.(type) {
		case string:
			if v != "" {
				// Split comma-delimited string
				repos := strings.Split(v, ",")
				for _, repo := range repos {
					repo = strings.TrimSpace(repo)
					if repo != "" {
						i.EnablerepoList = append(i.EnablerepoList, repo)
					}
				}
			}
		case []interface{}:
			for idx, item := range v {
				if repoStr, ok := item.(string); ok {
					if repoStr == "" {
						return fmt.Errorf("enablerepo name at index %d cannot be empty", idx)
					}
					i.EnablerepoList = append(i.EnablerepoList, repoStr)
				} else {
					return fmt.Errorf("invalid type for enablerepo name at index %d: expected string, got %T", idx, item)
				}
			}
		case []string:
			for idx, repoStr := range v {
				if repoStr == "" {
					return fmt.Errorf("enablerepo name at index %d cannot be empty", idx)
				}
				i.EnablerepoList = append(i.EnablerepoList, repoStr)
			}
		default:
			return fmt.Errorf("invalid type for 'enablerepo' parameter: expected string, comma-delimited string, or list of strings, got %T", i.Enablerepo)
		}
	}

	// Parse Disablerepo
	if i.Disablerepo != nil {
		switch v := i.Disablerepo.(type) {
		case string:
			if v != "" {
				// Split comma-delimited string
				repos := strings.Split(v, ",")
				for _, repo := range repos {
					repo = strings.TrimSpace(repo)
					if repo != "" {
						i.DisablerepoList = append(i.DisablerepoList, repo)
					}
				}
			}
		case []interface{}:
			for idx, item := range v {
				if repoStr, ok := item.(string); ok {
					if repoStr == "" {
						return fmt.Errorf("disablerepo name at index %d cannot be empty", idx)
					}
					i.DisablerepoList = append(i.DisablerepoList, repoStr)
				} else {
					return fmt.Errorf("invalid type for disablerepo name at index %d: expected string, got %T", idx, item)
				}
			}
		case []string:
			for idx, repoStr := range v {
				if repoStr == "" {
					return fmt.Errorf("disablerepo name at index %d cannot be empty", idx)
				}
				i.DisablerepoList = append(i.DisablerepoList, repoStr)
			}
		default:
			return fmt.Errorf("invalid type for 'disablerepo' parameter: expected string, comma-delimited string, or list of strings, got %T", i.Disablerepo)
		}
	}

	// Parse Exclude
	if i.Exclude != nil {
		switch v := i.Exclude.(type) {
		case string:
			if v != "" {
				// Split comma-delimited string
				pkgs := strings.Split(v, ",")
				for _, pkgName := range pkgs {
					pkgName = strings.TrimSpace(pkgName)
					if pkgName != "" {
						i.ExcludeList = append(i.ExcludeList, pkgName)
					}
				}
			}
		case []interface{}:
			for idx, item := range v {
				if pkgName, ok := item.(string); ok {
					if pkgName == "" {
						return fmt.Errorf("exclude package name at index %d cannot be empty", idx)
					}
					i.ExcludeList = append(i.ExcludeList, pkgName)
				} else {
					return fmt.Errorf("invalid type for exclude package name at index %d: expected string, got %T", idx, item)
				}
			}
		case []string:
			for idx, pkgName := range v {
				if pkgName == "" {
					return fmt.Errorf("exclude package name at index %d cannot be empty", idx)
				}
				i.ExcludeList = append(i.ExcludeList, pkgName)
			}
		default:
			return fmt.Errorf("invalid type for 'exclude' parameter: expected string, comma-delimited string, or list of strings, got %T", i.Exclude)
		}
	}

	return nil
}

// Validate checks if the input parameters are valid.
func (i *YumInput) Validate() error {
	if err := i.parseAndValidatePackages(); err != nil {
		return err
	}

	if err := i.parseAndValidateAll(); err != nil {
		return err
	}

	if i.State == "" {
		i.State = "present" // Default state
	}
	switch i.State {
	case "present", "absent", "latest", "installed", "removed":
		// Valid states
	default:
		return fmt.Errorf("invalid state %q for yum module, must be one of: present, absent, latest, installed, removed", i.State)
	}
	return nil
}

// HasRevert indicates if the action can be reverted.
func (i *YumInput) HasRevert() bool {
	// Can revert install/remove if we have package names.
	// update_cache is not easily revertible.
	return len(i.PkgNames) > 0 && (i.State == "present" || i.State == "installed" || i.State == "absent" || i.State == "removed")
}

// String provides a string representation of the YumOutput.
func (o YumOutput) String() string {
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

// Changed indicates if the yum module made changes to the system.
func (o YumOutput) Changed() bool {
	return o.WasChanged
}

// AsFacts implements the pkg.FactProvider interface.
func (o YumOutput) AsFacts() map[string]interface{} {
	facts := map[string]interface{}{}
	if len(o.Packages) > 0 {
		facts["packages"] = o.Packages
	}
	if len(o.Versions) > 0 {
		facts["versions"] = o.Versions
	}
	facts["state"] = o.State
	facts["changed"] = o.Changed()
	facts["update_cache"] = o.UpdateCache
	return facts
}

// runYumCommand executes a yum command, handling sudo and common options.
func runYumCommand(c *pkg.HostContext, runAsUser string, enablerepo, disablerepo, exclude []string, args ...string) (string, string, bool, error) {
	baseCmd := []string{
		"yum",
		"-y", // Assume yes
		"-q", // Quiet mode
	}

	// Add repository options
	if len(enablerepo) > 0 {
		for _, repo := range enablerepo {
			baseCmd = append(baseCmd, "--enablerepo="+repo)
		}
	}
	if len(disablerepo) > 0 {
		for _, repo := range disablerepo {
			baseCmd = append(baseCmd, "--disablerepo="+repo)
		}
	}

	// Add exclude options
	if len(exclude) > 0 {
		for _, pkgName := range exclude {
			baseCmd = append(baseCmd, "--exclude="+pkgName)
		}
	}

	fullCmd := append(baseCmd, args...)
	cmdString := strings.Join(fullCmd, " ")

	_, stdout, stderr, err := c.RunCommand(cmdString, runAsUser)

	if err != nil {
		return stdout, stderr, false, fmt.Errorf("yum command failed: %w\nStderr: %s", err, stderr)
	}

	// Check if yum reported changes
	// Look for lines indicating installation, removal, or upgrade
	changed := strings.Contains(stdout, "Installing") ||
		strings.Contains(stdout, "Installed") ||
		strings.Contains(stdout, "Removing") ||
		strings.Contains(stdout, "Removed") ||
		strings.Contains(stdout, "Updating") ||
		strings.Contains(stdout, "Updated") ||
		strings.Contains(stdout, "Complete!") ||
		strings.Contains(stderr, "Installing") ||
		strings.Contains(stderr, "Installed") ||
		strings.Contains(stderr, "Removing") ||
		strings.Contains(stderr, "Removed") ||
		strings.Contains(stderr, "Updating") ||
		strings.Contains(stderr, "Updated")

	return stdout, stderr, changed, nil
}

// isYumPackageInstalled checks if a package is installed using rpm -q.
func isYumPackageInstalled(c *pkg.HostContext, pkgName string) (bool, error) {
	// rpm -q is a reliable way to check package status
	cmdString := fmt.Sprintf("rpm -q %s", pkgName)
	_, stdout, stderr, err := c.RunCommand(cmdString, "")

	if err != nil {
		// RPM returns exit code 1 if package is not found, which is not an "error" for our check
		if strings.Contains(stderr, "is not installed") || strings.Contains(stdout, "is not installed") {
			return false, nil
		} else if strings.Contains(err.Error(), "exit status 1") {
			// Other exit status 1 might also mean not installed
			return false, nil
		}
		// Any other error is unexpected
		return false, fmt.Errorf("rpm -q failed: %w\nStderr: %s", err, stderr)
	}

	// If rpm -q succeeds, the package is installed
	return true, nil
}

// Execute runs the yum module logic.
func (m YumModule) Execute(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, runAs string) (pkg.ModuleOutput, error) {
	yumParams, ok := params.(*YumInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Execute: params is nil, expected *YumInput but got nil")
		}
		return nil, fmt.Errorf("Execute: incorrect parameter type: expected *YumInput, got %T", params)
	}

	if err := yumParams.Validate(); err != nil {
		return nil, err
	}

	output := YumOutput{Packages: yumParams.PkgNames, UpdateCache: yumParams.UpdateCache}
	var overallChanged bool

	// Template package names
	templatedPkgNames := []string{}
	for _, name := range yumParams.PkgNames {
		templatedName, err := pkg.TemplateString(name, closure)
		if err != nil {
			return nil, fmt.Errorf("failed to template package name '%s': %w", name, err)
		}
		templatedPkgNames = append(templatedPkgNames, templatedName)
	}
	output.Packages = templatedPkgNames

	// Template repository names
	templatedEnablerepo := []string{}
	for _, repo := range yumParams.EnablerepoList {
		templatedRepo, err := pkg.TemplateString(repo, closure)
		if err != nil {
			return nil, fmt.Errorf("failed to template enablerepo '%s': %w", repo, err)
		}
		templatedEnablerepo = append(templatedEnablerepo, templatedRepo)
	}

	templatedDisablerepo := []string{}
	for _, repo := range yumParams.DisablerepoList {
		templatedRepo, err := pkg.TemplateString(repo, closure)
		if err != nil {
			return nil, fmt.Errorf("failed to template disablerepo '%s': %w", repo, err)
		}
		templatedDisablerepo = append(templatedDisablerepo, templatedRepo)
	}

	// Template exclude packages
	templatedExclude := []string{}
	for _, pkgName := range yumParams.ExcludeList {
		templatedPkgName, err := pkg.TemplateString(pkgName, closure)
		if err != nil {
			return nil, fmt.Errorf("failed to template exclude package '%s': %w", pkgName, err)
		}
		templatedExclude = append(templatedExclude, templatedPkgName)
	}

	if yumParams.UpdateCache {
		common.LogDebug("Updating yum cache", map[string]interface{}{"host": closure.HostContext.Host.Name})
		_, _, cacheChanged, err := runYumCommand(closure.HostContext, runAs, templatedEnablerepo, templatedDisablerepo, templatedExclude, "update")
		if err != nil {
			return nil, fmt.Errorf("failed to update yum cache: %w", err)
		}
		overallChanged = overallChanged || cacheChanged
	}

	if len(templatedPkgNames) == 0 || yumParams.UpdateOnly {
		// Only update_cache was requested
		output.State = "cache_updated"
		output.WasChanged = overallChanged
		return output, nil
	}

	// Package State Logic
	pkgsToInstall := []string{}
	pkgsToRemove := []string{}
	finalState := yumParams.State

	for _, pkgName := range templatedPkgNames {
		installed, err := isYumPackageInstalled(closure.HostContext, pkgName)
		if err != nil {
			return nil, fmt.Errorf("failed to check package status for %s: %w", pkgName, err)
		}

		switch yumParams.State {
		case "present", "latest", "installed":
			if !installed || yumParams.State == "latest" {
				pkgsToInstall = append(pkgsToInstall, pkgName)
			} else {
				common.LogDebug("Package already present", map[string]interface{}{"host": closure.HostContext.Host.Name, "package": pkgName})
			}
		case "absent", "removed":
			if installed {
				pkgsToRemove = append(pkgsToRemove, pkgName)
			} else {
				common.LogDebug("Package already absent", map[string]interface{}{"host": closure.HostContext.Host.Name, "package": pkgName})
			}
		}
	}

	// Execute yum commands if needed
	if len(pkgsToInstall) > 0 {
		common.LogDebug("Ensuring packages are present/latest", map[string]interface{}{"host": closure.HostContext.Host.Name, "packages": pkgsToInstall, "state": yumParams.State})
		args := append([]string{"install"}, pkgsToInstall...)
		_, _, changed, err := runYumCommand(closure.HostContext, runAs, templatedEnablerepo, templatedDisablerepo, templatedExclude, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to install/upgrade packages %v: %w", pkgsToInstall, err)
		}
		overallChanged = overallChanged || changed
		finalState = "installed"
	}

	if len(pkgsToRemove) > 0 {
		common.LogDebug("Ensuring packages are absent", map[string]interface{}{"host": closure.HostContext.Host.Name, "packages": pkgsToRemove})
		args := append([]string{"remove"}, pkgsToRemove...)
		_, _, changed, err := runYumCommand(closure.HostContext, runAs, templatedEnablerepo, templatedDisablerepo, templatedExclude, args...)
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
func (m YumModule) Revert(params pkg.ConcreteModuleInputProvider, closure *pkg.Closure, previous pkg.ModuleOutput, runAs string) (pkg.ModuleOutput, error) {
	yumParams, ok := params.(*YumInput)
	if !ok {
		if params == nil {
			return nil, fmt.Errorf("Revert: params is nil, expected *YumInput but got nil")
		}
		return nil, fmt.Errorf("Revert: incorrect parameter type: expected *YumInput, got %T", params)
	}

	// Ensure PkgNames is populated
	if yumParams.PkgNames == nil {
		if err := yumParams.parseAndValidatePackages(); err != nil {
			return nil, fmt.Errorf("internal state error: packages not parsed/validated before Revert: %w", err)
		}
	}

	// Ensure repository lists are populated
	if yumParams.EnablerepoList == nil || yumParams.DisablerepoList == nil {
		if err := yumParams.parseAndValidateAll(); err != nil {
			return nil, fmt.Errorf("internal state error: repositories not parsed/validated before Revert: %w", err)
		}
	}

	// Re-template package names for revert context
	templatedPkgNames := []string{}
	if yumParams.PkgNames != nil {
		for _, name := range yumParams.PkgNames {
			templatedName, err := pkg.TemplateString(name, closure)
			if err != nil {
				return nil, fmt.Errorf("failed to template package name '%s' for revert: %w", name, err)
			}
			templatedPkgNames = append(templatedPkgNames, templatedName)
		}
	}

	// Re-template repository names
	templatedEnablerepo := []string{}
	for _, repo := range yumParams.EnablerepoList {
		templatedRepo, err := pkg.TemplateString(repo, closure)
		if err != nil {
			return nil, fmt.Errorf("failed to template enablerepo '%s' for revert: %w", repo, err)
		}
		templatedEnablerepo = append(templatedEnablerepo, templatedRepo)
	}

	templatedDisablerepo := []string{}
	for _, repo := range yumParams.DisablerepoList {
		templatedRepo, err := pkg.TemplateString(repo, closure)
		if err != nil {
			return nil, fmt.Errorf("failed to template disablerepo '%s' for revert: %w", repo, err)
		}
		templatedDisablerepo = append(templatedDisablerepo, templatedRepo)
	}

	// Re-template exclude packages for revert context
	templatedExclude := []string{}
	if yumParams.ExcludeList != nil {
		for _, pkgName := range yumParams.ExcludeList {
			templatedPkgName, err := pkg.TemplateString(pkgName, closure)
			if err != nil {
				return nil, fmt.Errorf("failed to template exclude package '%s' for revert: %w", pkgName, err)
			}
			templatedExclude = append(templatedExclude, templatedPkgName)
		}
	}

	if len(templatedPkgNames) == 0 || !yumParams.HasRevert() {
		return YumOutput{State: "norevert"}, nil
	}

	common.LogInfo("Reverting yum task", map[string]interface{}{
		"host":     closure.HostContext.Host.Name,
		"packages": templatedPkgNames,
		"state":    yumParams.State,
	})

	var revertAction string
	var pkgsForRevertAction []string

	switch yumParams.State {
	case "present", "installed":
		revertAction = "remove"
		pkgsForRevertAction = templatedPkgNames
	case "absent", "removed":
		revertAction = "install"
		pkgsForRevertAction = templatedPkgNames
	default:
		return YumOutput{State: "norevert", Packages: templatedPkgNames}, fmt.Errorf("cannot revert state %q for packages %v", yumParams.State, templatedPkgNames)
	}

	args := append([]string{revertAction}, pkgsForRevertAction...)
	_, _, changed, err := runYumCommand(closure.HostContext, runAs, templatedEnablerepo, templatedDisablerepo, templatedExclude, args...)
	if err != nil {
		return YumOutput{Packages: pkgsForRevertAction, State: fmt.Sprintf("revert_failed_%s", revertAction), WasChanged: false}, fmt.Errorf("revert command '%s' failed for packages %v: %w", revertAction, pkgsForRevertAction, err)
	}

	return YumOutput{Packages: pkgsForRevertAction, State: fmt.Sprintf("reverted_%s", revertAction), WasChanged: changed}, nil
}

// UnmarshalYAML handles shorthand and list formats for the yum module 'name'.
func (i *YumInput) UnmarshalYAML(node *yaml.Node) error {
	// Default values
	i.State = "present"
	i.UpdateCache = false

	if node.Kind == yaml.ScalarNode && (node.Tag == "!!str" || node.Tag == "") {
		// Shorthand: yum: package_name (implies state: present)
		i.Name = node.Value
		return i.parseAndValidatePackages()
	}

	if node.Kind == yaml.MappingNode {
		// Use a temporary type to avoid recursion
		type YumInputMap struct {
			Name        interface{} `yaml:"name"` // Accept string or list
			Pkg         interface{} `yaml:"pkg"`  // Alias, accept string or list
			State       string      `yaml:"state"`
			UpdateCache *bool       `yaml:"update_cache"` // Use pointer for explicit false
			Enablerepo  interface{} `yaml:"enablerepo"`
			Disablerepo interface{} `yaml:"disablerepo"`
			Exclude     interface{} `yaml:"exclude"`
		}
		var tmp YumInputMap
		if err := node.Decode(&tmp); err != nil {
			return fmt.Errorf("failed to decode yum input map (line %d): %w", node.Line, err)
		}

		if tmp.Name != nil && tmp.Pkg != nil {
			return fmt.Errorf("cannot specify both 'name' and 'pkg' for yum module (line %d)", node.Line)
		}

		if tmp.Pkg != nil {
			i.Name = tmp.Pkg
		} else {
			i.Name = tmp.Name
		}

		if tmp.State != "" {
			i.State = tmp.State
		}
		if tmp.UpdateCache != nil {
			i.UpdateCache = *tmp.UpdateCache
		}
		if tmp.Enablerepo != nil {
			i.Enablerepo = tmp.Enablerepo
		}
		if tmp.Disablerepo != nil {
			i.Disablerepo = tmp.Disablerepo
		}
		if tmp.Exclude != nil {
			i.Exclude = tmp.Exclude
		}

		// Parse and validate both packages and repositories
		if err := i.parseAndValidatePackages(); err != nil {
			return err
		}
		if err := i.parseAndValidateAll(); err != nil {
			return err
		}

		return nil
	}

	return fmt.Errorf("invalid type for yum module input (line %d): expected string or map, got %s", node.Line, node.Tag)
}

func init() {
	pkg.RegisterModule("yum", YumModule{})
	pkg.RegisterModule("ansible.builtin.yum", YumModule{})
	pkg.RegisterModule("dnf", YumModule{})
	pkg.RegisterModule("ansible.builtin.dnf", YumModule{})
}

// ParameterAliases defines aliases for module parameters.
func (m YumModule) ParameterAliases() map[string]string {
	return map[string]string{}
}
