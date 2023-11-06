package plumber

import (
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
