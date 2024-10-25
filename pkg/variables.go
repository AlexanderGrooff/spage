package pkg

import (
	"bytes"
	"fmt"
	"text/template"
)

type Context map[string]interface{}

func (c Context) TemplateString(s string) string {
	tmpl, err := template.New("tmpl").Parse(s)
	fmt.Printf("Templating %s with context %v\n", s, c)
	if err != nil {
		return "" // Handle error appropriately in real code
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, c)
	if err != nil {
		return "" // Handle error appropriately in real code
	}

	return buf.String()
}
