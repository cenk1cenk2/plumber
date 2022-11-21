package plumber

import (
	"text/template"

	sprig "github.com/go-task/slim-sprig"
)

func TemplateFuncMap() template.FuncMap {
	return sprig.FuncMap()
}
