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
	// TODO: track if file was created
	pkg.ModuleOutput
}

func (i TemplateInput) ToCode(indent int) string {
	return fmt.Sprintf("modules.TemplateInput{Src: %q, Dest: %q}",
		i.Src,
		i.Dest,
	)
}

func (o TemplateOutput) String() string {
	// TODO: show diff
	return fmt.Sprintf("  original: %q\n  new: %q", o.OriginalContents, o.NewContents)
}

func (o TemplateOutput) Changed() bool {
	return o.OriginalContents != o.NewContents
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

	originalContents, err := c.ReadFile(dest)
	if err != nil {
		return "", "", fmt.Errorf("failed to read original contents of %s: %s", dest, err)
	}
	if err := c.WriteFile(dest, templatedContents); err != nil {
		return "", "", fmt.Errorf("failed to write to file %s: %v", dest, err)
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

func (s TemplateModule) Revert(params interface{}, c pkg.Context, previous interface{}) (interface{}, error) {
	// TODO: delete if previously created?
	p := params.(TemplateInput)
	if previous != nil {
		prev := previous.(TemplateOutput)
		if prev.Changed() {
			if err := c.WriteFile(p.Dest, prev.OriginalContents); err != nil {
				return TemplateOutput{}, fmt.Errorf("failed to place back original contents in %s", p.Dest)
			}
		}
		return TemplateOutput{OriginalContents: prev.NewContents, NewContents: prev.OriginalContents}, nil
	}
	fmt.Printf("Not reverting because previous result was %v", previous)
	return TemplateOutput{}, nil
}

func init() {
	pkg.RegisterModule("template", TemplateModule{})
}
