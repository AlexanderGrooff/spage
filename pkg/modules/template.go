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
	OriginalContents string
	NewContents      string
	pkg.ModuleOutput
}

func (i TemplateInput) ToCode(indent int) string {
	return fmt.Sprintf("modules.TemplateInput{Src: %q, Dest: %q},",
		i.Src,
		i.Dest,
	)
}

func (o TemplateOutput) String() string {
	// TODO: show diff
	return fmt.Sprintf("  original: %q\n  new: %q", o.OriginalContents, o.NewContents)
}

func (o TemplateOutput) Changed() bool {
	return o.OriginalContents == o.NewContents
}

func templateContentsToFile(src, dest string, c pkg.Context) (string, string, error) {
	// Get contents from src
	contents, err := c.ReadTemplateFile(src)
	if err != nil {
		return "", "", fmt.Errorf("failed to read template file %s: %v", src, err)
	}
	templatedContents, err := c.TemplateString(contents)
	if err != nil {
		return "", "", err
	}

	var originalContents string
	if c.Host.IsLocal {
		originalContents, err = c.ReadLocalFile(dest)
		if err != nil {
			return "", "", err
		}
		if err := c.WriteLocalFile(dest, templatedContents); err != nil {
			return "", "", fmt.Errorf("failed to write to local file %s: %v", dest, err)
		}
	} else {
		originalContents, err = c.ReadRemoteFile(dest)
		if err != nil {
			return "", "", err
		}
		if err := c.WriteRemoteFile(c.Host.Host, dest, templatedContents); err != nil {
			return "", "", fmt.Errorf("failed to write remote file %s on host %s: %v", dest, c.Host.Host, err)
		}
	}
	return originalContents, templatedContents, nil
}

func (s TemplateModule) Execute(params interface{}, c pkg.Context) (interface{}, error) {
	p := params.(TemplateInput)
	original, new, err := templateContentsToFile(p.Src, p.Dest, c)
	if err != nil {
		return nil, err
	}
	return TemplateOutput{
		OriginalContents: original,
		NewContents:      new,
	}, nil
}

func (s TemplateModule) Revert(params interface{}, c pkg.Context) (interface{}, error) {
	// TODO: place back previous file contents
	return TemplateOutput{}, nil
}

func init() {
	pkg.RegisterModule("template", TemplateModule{})
}
