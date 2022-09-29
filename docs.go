package plumber

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/urfave/cli/v2"
)

type parsedFlags = map[string][]*templateFlag

type templateCommand struct {
	Name        string
	Aliases     []string
	Flags       parsedFlags
	Usage       string
	Description string
}

type templateFlag struct {
	Name        []string
	Description string
	Type        string
	Required    bool
	Default     string
	Category    string
	Format      string
}

type markdownTemplateInput struct {
	App         *cli.App
	GlobalFlags parsedFlags
	Commands    []*templateCommand
}

//go:embed templates
var templates embed.FS

func (p *Plumber) generateMarkdownDocumentation() error {
	// const start = "<!-- clidocs -->"
	// const end = "<!-- clidocsstop -->"
	// expr := fmt.Sprintf(`(?s)%s(.*)%s`, start, end)
	//
	// p.Log.Debugf("Using expression: %s", expr)

	data, err := p.toMarkdown()

	if err != nil {
		return err
	}

	// p.Log.Infof("Trying to read file: %s", p.readme)
	//
	// content, err := os.ReadFile(p.readme)
	//
	// if err != nil {
	// 	return err
	// }
	//
	// readme := string(content)
	//
	// r := regexp.MustCompile(expr)
	//
	// replace := strings.Join([]string{start, "", data, "", end}, "\n")
	//
	// result := r.ReplaceAllString(readme, replace)

	err = os.WriteFile(p.DocsFile, []byte(data), 0600)

	if err != nil {
		return err
	}

	p.Log.Infof("Wrote to file: %s", p.DocsFile)

	return nil
}

func (p *Plumber) toMarkdown() (string, error) {
	var w bytes.Buffer
	const name = "templates/markdown.go.tmpl"
	tmpl, err := templates.ReadFile(name)

	if err != nil {
		return "", err
	}

	t, err := template.New(name).Funcs(template.FuncMap{"StringsJoin": strings.Join}).Parse(string(tmpl))

	if err != nil {
		return "", err
	}

	err = t.ExecuteTemplate(&w, name, &markdownTemplateInput{
		App:         p.Cli,
		Commands:    p.toMarkdownCommand(p.Cli.Commands),
		GlobalFlags: p.toMarkdownFlags(p.Cli.VisibleFlags()),
	})

	return w.String(), err
}

func (p *Plumber) toMarkdownCommand(commands []*cli.Command) []*templateCommand {
	var processed []*templateCommand

	for _, command := range commands {
		if command.Hidden {
			continue
		}

		parsed := &templateCommand{
			Name:        command.FullName(),
			Aliases:     command.Aliases,
			Description: command.Description,
			Usage:       command.Usage,
			Flags:       p.toMarkdownFlags(command.VisibleFlags()),
		}

		if p.DocsExcludeHelpCommand && parsed.Name == "help" {
			continue
		}

		processed = append(processed, parsed)

		p.Log.Debugf("Processed command: %+v", parsed)

		if len(command.Subcommands) > 0 {
			processed = append(
				processed,
				p.toMarkdownCommand(command.Subcommands)...,
			)
		}
	}

	return processed
}

func (p *Plumber) toMarkdownFlags(
	flags []cli.Flag,
) parsedFlags {
	all := []*templateFlag{}
	processed := parsedFlags{}

	for _, f := range flags {
		current, ok := f.(cli.DocGenerationFlag)

		if !ok {
			p.Log.Errorf("Is not a valid flag: %s", f.String())

			continue
		}

		names := []string{}
		if !p.DocsExcludeFlags {
			for _, s := range current.Names() {
				trimmed := strings.TrimSpace(s)

				if len(trimmed) > 1 {
					names = append(names, fmt.Sprintf("--%s", trimmed))
				} else {
					names = append(names, fmt.Sprintf("-%s", trimmed))
				}
			}
		}

		if !p.DocsExcludeEnvironmentVariables {
			for _, v := range current.GetEnvVars() {
				names = append(names, fmt.Sprintf("$%s", v))
			}
		}

		description := current.GetUsage()

		re := regexp.MustCompile("((format|dynamic|enum).*)$")

		format := re.FindString(description)

		description = re.ReplaceAllString(description, "")

		parsed := &templateFlag{
			Name:        names,
			Description: description,
			Type:        strings.ReplaceAll(strings.ReplaceAll(reflect.TypeOf(f).String(), "*cli.", ""), "Flag", ""),
			Format:      format,
			Default:     current.GetDefaultText(),
			Required:    current.(cli.RequiredFlag).IsRequired(),
			Category:    current.(cli.CategorizableFlag).GetCategory(),
		}

		if len(parsed.Name) == 0 {
			p.Log.Debugf("Skipped flag: %+v", parsed)

			continue
		}

		p.Log.Debugf("Processed flag: %+v", parsed)

		all = append(
			all,
			parsed,
		)
	}

	for _, flag := range all {
		category := "EMPTY"

		if flag.Category != "" {
			category = flag.Category
		}

		if _, ok := processed[category]; !ok {
			processed[category] = []*templateFlag{}
		}

		processed[category] = append(processed[category], flag)
	}

	return processed
}
