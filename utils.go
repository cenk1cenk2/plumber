package plumber

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"

	sprig "github.com/go-task/slim-sprig/v3"
	"github.com/urfave/cli/v2"
	"golang.org/x/exp/slices"
)

func TemplateFuncMap() template.FuncMap {
	return sprig.FuncMap()
}

func EditCliFlag[Flag any](flags []cli.Flag, fn func(f Flag) bool, apply func(f Flag) Flag) []cli.Flag {
	clone := slices.Clone(flags)

	OverwriteCliFlag(clone, fn, apply)

	return clone
}

func OverwriteCliFlag[Flag any](flags []cli.Flag, fn func(f Flag) bool, apply func(f Flag) Flag) {
	index := slices.IndexFunc(flags, func(flag cli.Flag) bool {
		converted, ok := flag.(Flag)

		if !ok {
			return false
		}

		return fn(converted)
	})

	if index < 0 {
		panic(fmt.Errorf("Flag can not be found to modify."))
	}

	applied := apply(flags[index].(Flag))

	cast := (interface{})(applied)

	modified, ok := cast.(cli.Flag)

	if !ok {
		panic(fmt.Errorf("Can not cast the type of the given flag."))
	}

	flags[index] = modified
}

func ParseEnvironmentVariablesToMap() map[string]string {
	vars := map[string]string{}

	for _, v := range os.Environ() {
		pair := strings.SplitN(v, "=", 2)

		key := pair[0]
		value := pair[1]

		vars[key] = value
	}

	return vars
}

func InlineTemplate[Ctx any](tmpl string, ctx Ctx) (string, error) {
	if tmpl == "" {
		return "", nil
	}

	// functions can be found here: https://go-task.github.io/slim-sprig/
	tmp, err := template.New("inline").Funcs(TemplateFuncMap()).Parse(tmpl)

	if err != nil {
		return "", fmt.Errorf("Can not create inline template: %w", err)
	}

	var w bytes.Buffer

	err = tmp.ExecuteTemplate(&w, "inline", ctx)

	if err != nil {
		return "", fmt.Errorf("Can not generate inline template: %w", err)
	}

	return w.String(), nil
}
