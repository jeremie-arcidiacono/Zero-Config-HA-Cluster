package monitor

import (
	"embed"
	"fmt"
	"html/template"
)

//go:embed templates/dashboard.tmpl
var templateFiles embed.FS

func parseTemplates() (*template.Template, error) {
	page, err := template.ParseFS(templateFiles, "templates/dashboard.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse dashboard template: %w", err)
	}

	return page, nil
}
