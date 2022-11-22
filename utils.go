package plumber

import (
	"fmt"
	"text/template"

	sprig "github.com/go-task/slim-sprig"
	"github.com/urfave/cli/v2"
	"golang.org/x/exp/slices"
)

func TemplateFuncMap() template.FuncMap {
	return sprig.FuncMap()
}

func OverwriteCliFlag[Flag any](flags []cli.Flag, fn func(f Flag) bool, apply func(f Flag) Flag) []cli.Flag {
	clone := slices.Clone(flags)

	index := slices.IndexFunc(clone, func(flag cli.Flag) bool {
		converted, ok := flag.(Flag)

		if !ok {
			return false
		}

		return fn(converted)
	})

	applied := apply(clone[index].(Flag))

	cast := (interface{})(applied)

	modified, ok := cast.(cli.Flag)

	if !ok {
		panic(fmt.Errorf("Can not cast the type of the given flag."))
	}

	clone[index] = modified

	return clone
}
