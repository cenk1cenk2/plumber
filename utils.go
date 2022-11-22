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
