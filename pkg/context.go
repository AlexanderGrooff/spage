package pkg

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/runtime"

	"github.com/flosch/pongo2"
)

// No longer using type aliases for History and Facts

// MergeFacts adds all key-value pairs from 'other' into 'target'.
// It assumes both are *sync.Map.
func MergeFacts(target, other *sync.Map) {
	if target == nil || other == nil {
		// Handle nil maps appropriately, perhaps return an error or initialize
		return
	}
	other.Range(func(key, value interface{}) bool {
		target.Store(key, value)
		return true
	})
}

// AddFact stores a single key-value pair into the target *sync.Map.
func AddFact(target *sync.Map, k string, v interface{}) {
	if target == nil {
		// Handle nil map
		return
	}
	target.Store(k, v)
}

// FactsToJinja2 converts a *sync.Map (representing Facts) into a pongo2.Context.
func FactsToJinja2(facts *sync.Map) pongo2.Context {
	ctx := pongo2.Context{}
	if facts == nil {
		return ctx // Return empty context if map is nil
	}
	facts.Range(func(key, value interface{}) bool {
		// Ensure key is a string for pongo2.Context
		if k, ok := key.(string); ok {
			ctx[k] = value
		} else {
			// Optionally log or handle non-string keys
			common.LogWarn("Non-string key found in Facts map during Jinja2 conversion", map[string]interface{}{"key": key})
		}
		return true
	})
	return ctx
}

type HostContext struct {
	Host    *Host
	Facts   *sync.Map
	History *sync.Map
}

func InitializeHostContext(host *Host) *HostContext {
	return &HostContext{
		Host:    host,
		Facts:   new(sync.Map),
		History: new(sync.Map),
	}
}

func ReadTemplateFile(filename string) (string, error) {
	if filename[0] != '/' {
		return ReadLocalFile("templates/" + filename)
	}
	return ReadLocalFile(filename)
}

func ReadLocalFile(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (c HostContext) ReadFile(filename string, username string) (string, error) {
	if c.Host.IsLocal {
		return ReadLocalFile(filename)
	}
	return c.ReadRemoteFile(filename, username)
}

func (c HostContext) ReadRemoteFile(filename string, username string) (string, error) {
	stdout, _, err := runtime.RunRemoteCommand(c.Host.Host, fmt.Sprintf("cat \"%s\"", filename), username)
	if err != nil {
		return "", err
	}
	return stdout, nil
}

func (c HostContext) WriteFile(filename, contents, username string) error {
	if c.Host.IsLocal {
		return runtime.WriteLocalFile(filename, contents)
	}
	return runtime.WriteRemoteFile(c.Host.Host, filename, contents, username)
}

func (c HostContext) Copy(src, dst string) error {
	if c.Host.IsLocal {
		return runtime.CopyLocal(src, dst)
	}
	return runtime.CopyRemote(src, dst)
}

func (c HostContext) Stat(path, runAs string) (string, string, error) {
	if c.Host.IsLocal {
		return runtime.StatLocal(path, runAs)
	}
	// TODO: run specific stat flags based on arch in HostContext
	return runtime.StatRemote(path, c.Host.Host, runAs)
}

func (c HostContext) RunCommand(command, username string) (string, string, error) {
	// TODO: why was this necessary?
	//if username == "" {
	//	user, err := user.Current()
	//	if err != nil {
	//		return "", "", fmt.Errorf("failed to get current user: %v", err)
	//	}
	//	username = user.Username
	//}
	if c.Host.IsLocal {
		return runtime.RunLocalCommand(command, username)
	}
	return runtime.RunRemoteCommand(c.Host.Host, command, username)
}

func EvaluateExpression(s string, additionalVars ...*sync.Map) (string, error) {
	// TODO: this is a hack to evaluate a string as a Jinja2 template
	return TemplateString(fmt.Sprintf("{{ %s }}", s), additionalVars...)
}

// TemplateString processes a Jinja2 template string with provided variables.
// additionalVars should be a slice of *sync.Map.
func TemplateString(s string, additionalVars ...*sync.Map) (string, error) {
	if s == "" {
		return "", nil
	}
	tmpl, err := pongo2.FromString(s)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %v", err)
	}

	// Merge all provided variable maps into one *sync.Map
	allVars := new(sync.Map)
	for _, v := range additionalVars {
		MergeFacts(allVars, v) // Use the standalone MergeFacts function
	}
	var buf bytes.Buffer
	// Convert the final *sync.Map to pongo2.Context before executing
	facts := FactsToJinja2(allVars)
	if err := tmpl.ExecuteWriter(facts, &buf); err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}
	res := buf.String()
	common.DebugOutput("Templated %q into %q with facts: %v", s, res, facts)
	return res, nil
}

func getVariablesFromJinjaString(jinjaString string) []string {
	// TODO: this parses the string within jinja brackets. Add the rest of the Jinja grammar
	filterApplication := regexp.MustCompile(`(.+) | (.+)`)
	attributeVariable := regexp.MustCompile(`(.+)\.(.+)`)

	var vars []string
	if filterApplication.MatchString(jinjaString) {
		common.DebugOutput("Found Jinja filter %v", jinjaString)
		for _, match := range filterApplication.FindAllStringSubmatch(jinjaString, -1) {
			vars = append(vars, getVariablesFromJinjaString(match[1])...)
		}
		return vars
	}
	if attributeVariable.MatchString(jinjaString) {
		common.DebugOutput("Found Jinja subattribute %v", jinjaString)
		for _, match := range attributeVariable.FindAllStringSubmatch(jinjaString, -1) {
			vars = append(vars, getVariablesFromJinjaString(match[1])...)
		}
		return vars
	}
	return append(vars, jinjaString)
}

func GetVariableUsageFromTemplate(s string) []string {
	everythingBetweenBrackets := regexp.MustCompile(`{{\s*([^{}\s]+)\s*}}`)
	matches := everythingBetweenBrackets.FindAllStringSubmatch(s, -1)

	var vars []string
	for _, match := range matches {
		jinjaVariables := getVariablesFromJinjaString(match[1])
		vars = append(vars, jinjaVariables...)
	}
	if len(vars) > 0 {
		common.DebugOutput("Found variables %v in %v", vars, s)
	}
	return vars
}

// Helper function (might need adjustment based on actual ModuleOutput type)
// This is a placeholder, assuming ModuleOutput can be stored directly.
// If ModuleOutput needs specific conversion before storing in sync.Map, adjust this.
func OutputToFacts(output ModuleOutput) interface{} {
	// Placeholder: Directly return the output.
	// If ModuleOutput is complex, you might need to extract specific fields.
	return output
}

// IsExpressionTruthy evaluates a rendered expression string according to Jinja2/Ansible truthiness rules.
// Explicit "true"/"false" (case-insensitive) are respected.
// Empty strings are false.
// All other non-empty strings are true.
func IsExpressionTruthy(renderedExpr string) bool {
	trimmedResult := strings.TrimSpace(renderedExpr)
	lowerTrimmedResult := strings.ToLower(trimmedResult)

	parsedBool, err := strconv.ParseBool(lowerTrimmedResult)
	if err == nil {
		// Explicit true/false
		return parsedBool
	}

	// Not "true" or "false", evaluate truthiness:
	// Empty string is false, everything else is true.
	return trimmedResult != ""
}
