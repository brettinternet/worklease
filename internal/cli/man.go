package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	urfavecli "github.com/urfave/cli/v3"
)

// WriteManPage renders the registered public command tree as a section 1 page.
func WriteManPage(w io.Writer, root *urfavecli.Command, date string) error {
	if w == nil || root == nil || strings.TrimSpace(date) == "" {
		return fmt.Errorf("writer, command, and date are required")
	}
	out, errOut := root.Writer, root.ErrWriter
	root.Writer, root.ErrWriter = io.Discard, io.Discard
	err := root.Run(context.Background(), []string{root.Name, "--help"})
	root.Writer, root.ErrWriter = out, errOut
	if err != nil {
		return fmt.Errorf("prepare command tree: %w", err)
	}

	var page bytes.Buffer
	fmt.Fprintf(&page, ".TH %s 1 \"%s\" \"%s\" \"User Commands\"\n", strings.ToUpper(root.Name), roffMacro(date), roffMacro(root.Name+" "+root.Version))
	section(&page, "NAME")
	text(&page, root.Name+" - "+root.Usage)
	section(&page, "SYNOPSIS")
	literal(&page, usage(root))
	section(&page, "DESCRIPTION")
	text(&page, manDescription(root.Description))
	section(&page, "QUICK START")
	literal(&page, "worklease acquire --path README.md\nworklease verify\nworklease exec -- git status\nworklease checkpoint --data '{\"phase\":\"done\"}'\nworklease release --reason done")
	section(&page, "GLOBAL OPTIONS")
	flags(&page, root)
	if examples := manExamples(root.Description); len(examples) > 0 {
		section(&page, "EXAMPLES")
		literal(&page, strings.Join(examples, "\n"))
	}
	section(&page, "COMMAND REFERENCE")
	for _, command := range visibleCommands(root) {
		fmt.Fprintf(&page, ".SS \"%s\"\n", roff(strings.Join(command.Path(), " ")))
		literal(&page, usage(command))
		text(&page, manDescription(command.Description))
		if len(command.VisibleFlags()) > 0 {
			fmt.Fprintln(&page, ".PP\nOptions:")
			flags(&page, command)
		}
		if examples := manExamples(command.Description); len(examples) > 0 {
			fmt.Fprintln(&page, ".PP\nExamples:")
			literal(&page, strings.Join(examples, "\n"))
		}
	}
	section(&page, "FILES")
	definition(&page, "$WORKLEASE_HOME/worklease.db", "Local SQLite authority. The directory and database are owner-private.")
	definition(&page, "$WORKLEASE_HOME/handles/", "Private authority-bound, session-scoped contextual handles.")
	section(&page, "SEE ALSO")
	definition(&page, "Documentation", "https://github.com/brettinternet/worklease")
	_, err = io.Copy(w, &page)
	return err
}

func visibleCommands(root *urfavecli.Command) []*urfavecli.Command {
	var result []*urfavecli.Command
	var visit func(*urfavecli.Command)
	visit = func(parent *urfavecli.Command) {
		for _, command := range parent.VisibleCommands() {
			result = append(result, command)
			visit(command)
		}
	}
	visit(root)
	return result
}

func usage(command *urfavecli.Command) string {
	if value := strings.TrimSpace(command.UsageText); value != "" {
		return value
	}
	value := strings.Join(command.Path(), " ")
	if command.ArgsUsage != "" {
		value += " " + command.ArgsUsage
	}
	return value
}

func manDescription(value string) string {
	if index := strings.Index(value, "\n\nExamples:"); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

func manExamples(value string) []string {
	index := strings.Index(value, "\n\nExamples:")
	if index < 0 {
		return nil
	}
	var result []string
	for _, line := range strings.Split(value[index+len("\n\nExamples:"):], "\n") {
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}
	return result
}

func flags(page *bytes.Buffer, command *urfavecli.Command) {
	seen := map[string]bool{}
	for _, flag := range command.VisibleFlags() {
		if flag == nil || len(flag.Names()) == 0 {
			continue
		}
		key := strings.Join(flag.Names(), "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		names := make([]string, 0, len(flag.Names()))
		for _, name := range flag.Names() {
			prefix := "--"
			if len(name) == 1 {
				prefix = "-"
			}
			names = append(names, prefix+name)
		}
		usage := ""
		if doc, ok := flag.(urfavecli.DocGenerationFlag); ok {
			if doc.TakesValue() {
				for i := range names {
					names[i] += " " + strings.ToUpper(doc.TypeName())
				}
			}
			usage = doc.GetUsage()
		}
		definition(page, strings.Join(names, ", "), usage)
	}
}

func section(page *bytes.Buffer, title string) { fmt.Fprintf(page, ".SH \"%s\"\n", roff(title)) }
func definition(page *bytes.Buffer, term, description string) {
	fmt.Fprintln(page, ".TP")
	fmt.Fprintf(page, ".B %s\n", roff(term))
	text(page, description)
}
func literal(page *bytes.Buffer, value string) {
	fmt.Fprintln(page, ".nf")
	for _, line := range strings.Split(strings.TrimSpace(value), "\n") {
		fmt.Fprintln(page, roff(line))
	}
	fmt.Fprintln(page, ".fi")
}
func text(page *bytes.Buffer, value string) {
	const width = 72
	for _, paragraph := range strings.Split(strings.TrimSpace(value), "\n\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			word = roff(word)
			if line != "" && len(line)+1+len(word) > width {
				fmt.Fprintln(page, line)
				line = word
				continue
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		if line != "" {
			fmt.Fprintln(page, line)
		}
	}
}
func roffMacro(value string) string {
	value = strings.ReplaceAll(value, `\`, `\e`)
	return strings.ReplaceAll(value, `"`, `\(dq`)
}
func roff(value string) string {
	value = strings.ReplaceAll(value, `\`, `\e`)
	value = strings.ReplaceAll(value, `-`, `\-`)
	value = strings.ReplaceAll(value, `"`, `\(dq`)
	if strings.HasPrefix(value, ".") || strings.HasPrefix(value, "'") {
		value = `\&` + value
	}
	return value
}
