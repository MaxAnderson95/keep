package cli

import (
	"strings"

	"github.com/urfave/cli/v2"
)

type flagSet map[string]bool

// reorderArgs returns args with each command's flags moved ahead of its
// positional operands. It leaves everything up to and including the subcommand
// untouched, stops permuting at a literal "--", and preserves order otherwise.
func reorderArgs(app *cli.App, args []string) []string {
	if len(args) < 2 {
		return args
	}
	commands := commandNames(app.Commands)
	globalValues := valueFlagNames(app.Flags)

	// Find the subcommand index, skipping global flags (and their values).
	cmdIdx := -1
	var cmd *cli.Command
	for i := 1; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if globalValues[tokName(a)] && !strings.Contains(a, "=") {
				i++ // skip its value
			}
			continue
		}
		if found := commands[a]; found != nil {
			cmdIdx = i
			cmd = found
		}
		break
	}
	if cmdIdx < 0 || cmdIdx == len(args)-1 {
		return args
	}
	commandValues := valueFlagNames(cmd.Flags)
	for name := range globalValues {
		commandValues[name] = true
	}

	head := args[:cmdIdx+1]
	tail := args[cmdIdx+1:]

	var flags, pos []string
	passthrough := false
	for i := 0; i < len(tail); i++ {
		a := tail[i]
		if passthrough {
			pos = append(pos, a)
			continue
		}
		if a == "--" {
			passthrough = true
			pos = append(pos, a)
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			if commandValues[tokName(a)] && !strings.Contains(a, "=") && i+1 < len(tail) {
				flags = append(flags, tail[i+1])
				i++
			}
			continue
		}
		pos = append(pos, a)
	}

	out := make([]string, 0, len(args))
	out = append(out, head...)
	out = append(out, flags...)
	out = append(out, pos...)
	return out
}

func commandNames(commands []*cli.Command) map[string]*cli.Command {
	res := map[string]*cli.Command{}
	for _, cmd := range commands {
		for _, name := range cmd.Names() {
			res[name] = cmd
		}
	}
	return res
}

func valueFlagNames(flags []cli.Flag) flagSet {
	res := flagSet{}
	for _, f := range flags {
		doc, ok := f.(interface{ TakesValue() bool })
		if !ok || !doc.TakesValue() {
			continue
		}
		for _, name := range f.Names() {
			if len(name) == 1 {
				res["-"+name] = true
			} else {
				res["--"+name] = true
			}
		}
	}
	return res
}

func tokName(token string) string {
	name, _, _ := strings.Cut(token, "=")
	return name
}
