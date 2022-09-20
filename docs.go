package plumber

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"os"
	"reflect"
	"strings"

	"github.com/urfave/cli/v2"
)

type markdownTemplateCommand struct {
	Name        string
	Aliases     []string
	Flags       []*markdownTemplateFlag
	Usage       string
	Description string
}

type markdownTemplateFlag struct {
	Name        []string
	Description string
	Type        string
	Required    bool
	Default     string
}

type markdownTemplateInput struct {
	App         *cli.App
	GlobalFlags []*markdownTemplateFlag
	Commands    []*markdownTemplateCommand
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

	err = os.WriteFile(p.readme, []byte(data), 0600)

	if err != nil {
		return err
	}

	p.Log.Infof("Wrote to file: %s", p.readme)

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

func (p *Plumber) toMarkdownCommand(commands []*cli.Command) []*markdownTemplateCommand {
	var processed []*markdownTemplateCommand

	for _, command := range commands {
		if command.Hidden {
			continue
		}

		parsed := &markdownTemplateCommand{
			Name:        command.FullName(),
			Aliases:     command.Aliases,
			Description: command.Description,
			Usage:       command.Usage,
			Flags:       p.toMarkdownFlags(command.VisibleFlags()),
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
) []*markdownTemplateFlag {
	processed := []*markdownTemplateFlag{}

	for _, f := range flags {
		current, ok := f.(cli.DocGenerationFlag)

		if !ok {
			p.Log.Errorf("Is not a valid flag: %s", f.String())

			continue
		}

		names := []string{}
		for _, s := range current.Names() {
			trimmed := strings.TrimSpace(s)

			if len(trimmed) > 1 {
				names = append(names, fmt.Sprintf("--%s", trimmed))
			} else {
				names = append(names, fmt.Sprintf("-%s", trimmed))
			}
		}

		for _, v := range current.GetEnvVars() {
			names = append(names, fmt.Sprintf("$%s", v))
		}

		parsed := &markdownTemplateFlag{
			Name:        names,
			Description: current.GetUsage(),
			Type:        strings.ReplaceAll(strings.ReplaceAll(reflect.TypeOf(f).String(), "*cli.", ""), "Flag", ""),
			Default:     current.GetDefaultText(),
			Required:    current.(cli.RequiredFlag).IsRequired(),
		}

		p.Log.Debugf("Processed flag: %+v", parsed)

		processed = append(
			processed,
			parsed,
		)
	}

	return processed
}
