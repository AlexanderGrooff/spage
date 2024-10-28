package modules

import (
	"fmt"
	"reflect"

	"github.com/AlexanderGrooff/spage/pkg"
)

type TemplateModule struct{}

func (sm TemplateModule) InputType() reflect.Type {
	return reflect.TypeOf(TemplateInput{})
}

func (sm TemplateModule) OutputType() reflect.Type {
	return reflect.TypeOf(TemplateOutput{})
}

type TemplateInput struct {
	Src  string `yaml:"src"`
	Dest string `yaml:"dest"`
	pkg.ModuleInput
}

type TemplateOutput struct {
	ExitCode int    `yaml:"exit_code"`
	Stdout   string `yaml:"stdout"`
	Stderr   string `yaml:"stderr"`
	Command  string `yaml:"command"`
	pkg.ModuleOutput
}

func (i TemplateInput) ToCode(indent int) string {
	return fmt.Sprintf("modules.TemplateInput{Src: \"%s\", Dest: \"%s\"},",
		i.Src,
		i.Dest,
	)
}

func (o TemplateOutput) String() string {
	// TODO: show diff
	return fmt.Sprintf("  cmd: %s\n  exitcode: %d\n  stdout: %s\n  stderr: %s\n", o.Command, o.ExitCode, o.Stdout, o.Stderr)
}

func templateContentsToFile(src, dest string, c pkg.Context) error {
	// Get contents from src
	contents, err := c.ReadTemplateFile(src)
	if err != nil {
		return fmt.Errorf("failed to read template file %s: %v", src, err)
	}
	templatedContents, err := c.TemplateString(contents)
	if err != nil {
		return err
	}

	if c.Host.IsLocal {
		if err := c.WriteLocalFile(dest, templatedContents); err != nil {
			return fmt.Errorf("failed to write to local file %s: %v", dest, err)
		}
	}
	if err := c.WriteRemoteFile(c.Host.Host, dest, templatedContents); err != nil {
		return fmt.Errorf("failed to write remote file %s on host %s: %v", dest, c.Host.Host, err)
	}
	return nil
}

func (s TemplateModule) Execute(params interface{}, c pkg.Context) (interface{}, error) {
	p := params.(TemplateInput)
	templateContentsToFile(p.Src, p.Dest, c)
	return TemplateOutput{}, nil
}

func (s TemplateModule) Revert(params interface{}, c pkg.Context) (interface{}, error) {
	// TODO: place back previous file contents
	return TemplateOutput{}, nil
}

func init() {
	pkg.RegisterModule("template", TemplateModule{})
}
