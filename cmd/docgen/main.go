package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	pkg "github.com/AlexanderGrooff/spage/pkg"
	_ "github.com/AlexanderGrooff/spage/pkg/modules"
)

type fieldDoc struct {
	Name       string
	YamlTag    string
	TypeString string
	Comment    string
	Required   bool
}

func typeString(t reflect.Type) string {
	if t == nil {
		return "<nil>"
	}
	// Deref pointer for readability
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Slice:
		return "[" + typeString(t.Elem()) + "]"
	case reflect.Map:
		return fmt.Sprintf("map[%s]%s", typeString(t.Key()), typeString(t.Elem()))
	case reflect.Interface:
		// Keep interface for e.g. apt.Name
		return "any"
	case reflect.Struct:
		return t.Name()
	default:
		return t.String()
	}
}

func parseStructComments(structName, dir string) map[string]string {
	comments := map[string]string{}
	fset := token.NewFileSet()
	// Parse all files in dir for comments
	pkgs, _ := parser.ParseDir(fset, dir, nil, parser.ParseComments)
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || ts.Name == nil || ts.Name.Name != structName {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, field := range st.Fields.List {
						name := ""
						if len(field.Names) > 0 {
							name = field.Names[0].Name
						} else {
							// embedded field
							continue
						}
						c := strings.TrimSpace(fieldComment(field))
						if c != "" {
							comments[name] = c
						}
					}
				}
			}
		}
	}
	return comments
}

func fieldComment(f *ast.Field) string {
	if f.Comment != nil {
		return f.Comment.Text()
	}
	if f.Doc != nil {
		return f.Doc.Text()
	}
	return ""
}

func yamlTag(sf reflect.StructField) (string, bool) {
	tag := sf.Tag.Get("yaml")
	if tag == "" {
		return "", false
	}
	parts := strings.Split(tag, ",")
	return parts[0], true
}

func fieldsFromType(t reflect.Type, srcDir string) []fieldDoc {
	// Deref pointer
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	comments := parseStructComments(t.Name(), srcDir)
	out := make([]fieldDoc, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" { // unexported
			continue
		}
		ytag, hasTag := yamlTag(sf)
		// Only include fields that explicitly have a yaml tag (and not '-') and are not anonymous
		if !hasTag || ytag == "-" || sf.Anonymous {
			continue
		}
		fd := fieldDoc{
			Name:       sf.Name,
			YamlTag:    ytag,
			TypeString: typeString(sf.Type),
			Comment:    comments[sf.Name],
			Required:   false, // heuristics later
		}
		out = append(out, fd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].YamlTag < out[j].YamlTag })
	return out
}

type paramDoc struct {
	fieldDoc
	Extra pkg.ParameterDoc
}

func mergeParamDocs(fields []fieldDoc, docs map[string]pkg.ParameterDoc) []paramDoc {
	byYaml := map[string]fieldDoc{}
	for _, f := range fields {
		key := f.YamlTag
		if key == "" {
			key = f.Name
		}
		byYaml[key] = f
	}
	keys := make([]string, 0, len(byYaml))
	for k := range byYaml {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]paramDoc, 0, len(keys))
	for _, k := range keys {
		var extra pkg.ParameterDoc
		if docs != nil {
			if d, ok := docs[k]; ok {
				extra = d
			}
		}
		out = append(out, paramDoc{fieldDoc: byYaml[k], Extra: extra})
	}
	return out
}

func writeModuleDoc(moduleName string, mod pkg.BaseModule, repoRoot, docsRoot string) error {
	inputT := mod.InputType()
	outputT := mod.OutputType()

	// Try to guess source dir for the module based on package path
	// All modules are in spage/pkg/modules
	srcDir := filepath.Join(repoRoot, "pkg", "modules")

	inputFields := fieldsFromType(inputT, srcDir)
	outputFields := fieldsFromType(outputT, srcDir)
	var inputParamDocs []paramDoc
	if pdp, ok := mod.(pkg.ParameterDocsProvider); ok {
		inputParamDocs = mergeParamDocs(inputFields, pdp.ParameterDocs())
	} else {
		inputParamDocs = mergeParamDocs(inputFields, nil)
	}

	b := &strings.Builder{}
	fmt.Fprintf(b, "### %s module\n\n", moduleName)

	// Optional module-provided docs via interface
	if mdp, ok := mod.(pkg.ModuleDocProvider); ok {
		fmt.Printf("Using module-provided docs for %s\n", moduleName)
		content := mdp.Doc()
		if strings.TrimSpace(content) != "" {
			if !strings.HasSuffix(content, "\n\n") {
				if strings.HasSuffix(content, "\n") {
					content += "\n"
				} else {
					content += "\n\n"
				}
			}
			fmt.Fprint(b, content)
		}
	}

	// Inputs table
	fmt.Fprintf(b, "**Inputs**\n\n")
	if len(inputParamDocs) == 0 {
		fmt.Fprintf(b, "No input parameters.\n\n")
	} else {
		fmt.Fprintln(b, "| Parameter | Type | Description | Required | Default | Choices |")
		fmt.Fprintln(b, "|---|---|---|---|---|---|")
		for _, p := range inputParamDocs {
			f := p.fieldDoc
			name := f.YamlTag
			if name == "" { // fallback to struct name
				name = f.Name
			}
			desc := strings.TrimSpace(strings.ReplaceAll(f.Comment, "\n", " "))
			if p.Extra.Description != "" {
				desc = p.Extra.Description
			}
			required := ""
			if p.Extra.Required != nil {
				if *p.Extra.Required {
					required = "true"
				} else {
					required = "false"
				}
			}
			def := p.Extra.Default
			choices := ""
			if len(p.Extra.Choices) > 0 {
				choices = strings.Join(p.Extra.Choices, ", ")
			}
			fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s |\n", name, f.TypeString, desc, required, def, choices)
		}
		fmt.Fprintln(b)
	}

	// Outputs table
	fmt.Fprintf(b, "**Outputs**\n\n")
	if len(outputFields) == 0 {
		fmt.Fprintf(b, "No outputs.\n\n")
	} else {
		fmt.Fprintln(b, "| Field | Type | Description |")
		fmt.Fprintln(b, "|---|---|---|")
		for _, f := range outputFields {
			// Skip embedded ModuleOutput
			if f.Name == "ModuleOutput" || f.YamlTag == "" && f.TypeString == "ModuleOutput" {
				continue
			}
			name := f.YamlTag
			if name == "" {
				name = f.Name
			}
			desc := strings.TrimSpace(strings.ReplaceAll(f.Comment, "\n", " "))
			fmt.Fprintf(b, "| %s | %s | %s |\n", name, f.TypeString, desc)
		}
		fmt.Fprintln(b)
	}

	// Parameter aliases if any
	aliases := mod.ParameterAliases()
	if len(aliases) > 0 {
		fmt.Fprintf(b, "**Parameter aliases**\n\n")
		// stable order
		ks := make([]string, 0, len(aliases))
		for k := range aliases {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			fmt.Fprintf(b, "- **%s** → %s\n", k, aliases[k])
		}
		fmt.Fprintln(b)
	}

	// Write file
	// normalize name to base name (strip collection prefix)
	base := moduleName
	if strings.Contains(base, ".") {
		parts := strings.Split(base, ".")
		base = parts[len(parts)-1]
	}
	outPath := filepath.Join(docsRoot, base+".md")
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	fmt.Printf("Writing doc for %s to %s\n", moduleName, outPath)
	return os.WriteFile(outPath, []byte(b.String()), 0o644)
}

func main() {
	repoRoot := flag.String("repo", ".", "path to spage repo root")
	docsDir := flag.String("out", "../spage-docs/docs/modules", "output docs directory")
	only := flag.String("only", "", "only this module name")
	flag.Parse()

	mods := pkg.ListRegisteredModules()
	// stable order unique base names; prefer canonical names without collection prefix
	names := make([]string, 0, len(mods))
	for name := range mods {
		if *only != "" && name != *only {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	// Prefer writing docs for canonical names once; skip collection aliases when base already processed
	seenBase := map[string]bool{}
	for _, name := range names {
		base := name
		if strings.Contains(base, ".") {
			parts := strings.Split(base, ".")
			base = parts[len(parts)-1]
		}
		if seenBase[base] {
			continue
		}
		mod := mods[name]
		if err := writeModuleDoc(base, mod, *repoRoot, *docsDir); err != nil {
			fmt.Fprintf(os.Stderr, "error generating doc for %s: %v\n", name, err)
			os.Exit(1)
		}
		seenBase[base] = true
	}
}
