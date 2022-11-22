package plumber

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"text/template"

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
	if p.options.documentation.MarkdownOutputFile == "" {
		p.options.documentation.MarkdownOutputFile = "README.md"
	}

	data, err := p.toMarkdown()

	if err != nil {
		return err
	}

	err = os.WriteFile(p.options.documentation.MarkdownOutputFile, []byte(data), 0600)

	if err != nil {
		return err
	}

	p.Log.Infof("Wrote to file: %s", p.options.documentation.MarkdownOutputFile)

	return nil
}

func (p *Plumber) embedMarkdownDocumentation() error {
	if p.options.documentation.EmbeddedMarkdownOutputFile == "" {
		p.options.documentation.EmbeddedMarkdownOutputFile = "README.md"
	}

	const start = "<!-- clidocs -->"
	const end = "<!-- clidocsstop -->"
	expr := fmt.Sprintf(`(?s)%s(.*)%s`, start, end)

	p.Log.Debugf("Using expression: %s", expr)

	data, err := p.toEmbededMarkdown()

	if err != nil {
		return err
	}

	p.Log.Infof("Trying to read file: %s", p.options.documentation.EmbeddedMarkdownOutputFile)

	content, err := os.ReadFile(p.options.documentation.EmbeddedMarkdownOutputFile)

	if err != nil {
		return err
	}

	readme := string(content)

	r := regexp.MustCompile(expr)

	replace := strings.Join([]string{start, "", data, "", end}, "\n")

	result := r.ReplaceAllString(readme, replace)

	err = os.WriteFile(p.options.documentation.EmbeddedMarkdownOutputFile, []byte(result), 0600)

	if err != nil {
		return err
	}

	p.Log.Infof("Embedded into file: %s", p.options.documentation.EmbeddedMarkdownOutputFile)

	return nil
}

func (p *Plumber) generateMarkdownTemplateCtx() *markdownTemplateInput {
	input := &markdownTemplateInput{
		App:         p.Cli,
		Commands:    p.generateDocCommands(p.Cli.Commands),
		GlobalFlags: p.generateDocFlags(p.Cli.VisibleFlags()),
	}

	return input
}

func (p *Plumber) toMarkdown() (string, error) {
	var w bytes.Buffer
	const name = "templates/markdown.go.tmpl"
	tmpl, err := templates.ReadFile(name)

	if err != nil {
		return "", err
	}

	t, err := template.New(name).
		Funcs(TemplateFuncMap()).
		Parse(string(tmpl))

	if err != nil {
		return "", err
	}

	input := p.generateMarkdownTemplateCtx()

	p.Log.Tracef("Executing the template: %+v", input)

	err = t.ExecuteTemplate(&w, name, input)

	return w.String(), err
}

func (p *Plumber) toEmbededMarkdown() (string, error) {
	var w bytes.Buffer
	const name = "templates/markdown-flags.go.tmpl"
	tmpl, err := templates.ReadFile(name)

	if err != nil {
		return "", err
	}

	t, err := template.New(name).
		Funcs(TemplateFuncMap()).
		Parse(string(tmpl))

	if err != nil {
		return "", err
	}

	input := p.generateMarkdownTemplateCtx()

	p.Log.Tracef("Executing the embedded template: %+v", input)

	err = t.ExecuteTemplate(&w, name, input)

	return w.String(), err
}

func (p *Plumber) generateDocCommands(commands []*cli.Command) []*templateCommand {
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
			Flags:       p.generateDocFlags(command.VisibleFlags()),
		}

		if p.options.documentation.ExcludeHelpCommand && parsed.Name == "help" {
			continue
		}

		processed = append(processed, parsed)

		p.Log.Debugf("Processed command: %+v", parsed)

		if len(command.Subcommands) > 0 {
			processed = append(
				processed,
				p.generateDocCommands(command.Subcommands)...,
			)
		}
	}

	return processed
}

func (p *Plumber) generateDocFlags(
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
		if !p.options.documentation.ExcludeFlags {
			for _, s := range current.Names() {
				trimmed := strings.TrimSpace(s)

				if len(trimmed) > 1 {
					names = append(names, fmt.Sprintf("--%s", trimmed))
				} else {
					names = append(names, fmt.Sprintf("-%s", trimmed))
				}
			}
		}

		if !p.options.documentation.ExcludeEnvironmentVariables {
			for _, v := range current.GetEnvVars() {
				names = append(names, fmt.Sprintf("$%s", v))
			}
		}

		description := current.GetUsage()

		re := regexp.MustCompile(`((format|json|Template|RegExp|enum|multiple)\(.*\))$`)

		format := re.FindString(description)

		description = re.ReplaceAllString(description, "")

		text := current.GetDefaultText()
		if text == "" {
			text = current.GetValue()
		}

		parsed := &templateFlag{
			Name:        names,
			Description: description,
			Type:        strings.ReplaceAll(strings.ReplaceAll(reflect.TypeOf(f).String(), "*cli.", ""), "Flag", ""),
			Format:      format,
			Default:     text,
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
