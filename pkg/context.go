package pkg

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
	"regexp"

	"github.com/flosch/pongo2"
)

type History map[string]ModuleOutput
type Facts map[string]interface{}

func (f *Facts) Merge(other Facts) {
	for key, value := range other {
		(*f)[key] = value
	}
}

func (f *Facts) Add(k string, v ModuleOutput) Facts {
	(*f)[k] = v
	return *f
}

func (f Facts) ToJinja2() pongo2.Context {
	// TODO: handle Changed()
	ctx := pongo2.Context{}
	for k, v := range f {
		ctx[k] = v
	}
	return ctx
}

type HostContext struct {
	Host    *Host
	Facts   Facts
	History History
}

func ReadTemplateFile(filename string) (string, error) {
	return ReadLocalFile("templates/" + filename)
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
	stdout, _, err := RunRemoteCommand(c.Host.Host, fmt.Sprintf("cat \"%s\"", filename), username)
	if err != nil {
		return "", err
	}
	return stdout, nil
}

func (c HostContext) WriteFile(filename, contents, username string) error {
	if c.Host.IsLocal {
		return WriteLocalFile(filename, contents)
	}
	return WriteRemoteFile(c.Host.Host, filename, contents, username)
}

func (c HostContext) RunCommand(command, username string) (string, string, error) {
	if username == "" {
		user, err := user.Current()
		if err != nil {
			return "", "", fmt.Errorf("failed to get current user: %v", err)
		}
		username = user.Username
	}
	if c.Host.IsLocal {
		return RunLocalCommand(command, username)
	}
	return RunRemoteCommand(c.Host.Host, command, username)
}

func TemplateString(s string, additionalVars ...Facts) (string, error) {
	tmpl, err := pongo2.FromString(s)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %v", err)
	}

	allVars := make(Facts)
	for _, v := range additionalVars {
		allVars.Merge(v)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteWriter(allVars.ToJinja2(), &buf); err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}

	return buf.String(), nil
}

func getVariablesFromJinjaString(jinjaString string) []string {
	// TODO: this parses the string within jinja brackets. Add the rest of the Jinja grammar
	filterApplication := regexp.MustCompile(`(.+) | (.+)`)
	attributeVariable := regexp.MustCompile(`(.+)\.(.+)`)

	var vars []string
	if filterApplication.MatchString(jinjaString) {
		DebugOutput("Found Jinja filter %v", jinjaString)
		for _, match := range filterApplication.FindAllStringSubmatch(jinjaString, -1) {
			vars = append(vars, getVariablesFromJinjaString(match[1])...)
		}
		return vars
	}
	if attributeVariable.MatchString(jinjaString) {
		DebugOutput("Found Jinja subattribute %v", jinjaString)
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
	return vars
}
