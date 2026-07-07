// Package cli wires keep's command surface (urfave/cli) over the orchestration
// in internal/keep. It is intentionally thin: parse flags, call keep, format.
package cli

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/keep"
)

// BuildInfo carries version metadata injected via ldflags.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Run builds and executes the keep CLI, returning a process exit code.
func Run(args []string, bi BuildInfo) int {
	app := newApp(bi)
	if err := app.Run(reorderArgs(app, args)); err != nil {
		if ec, ok := err.(cli.ExitCoder); ok {
			if ec.Error() != "" {
				fmt.Fprintln(os.Stderr, "keep:", ec.Error())
			}
			return ec.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "keep:", err)
		return 1
	}
	return 0
}

func newApp(bi BuildInfo) *cli.App {
	configFlag := &cli.StringFlag{
		Name:    "config",
		Aliases: []string{"c"},
		Usage:   "path to the keep `CONFIG` (default ~/.config/keep/config.yaml)",
		EnvVars: []string{"KEEP_CONFIG"},
	}

	app := &cli.App{
		Name:                 "keep",
		Usage:                "declare and manage background services on macOS, launchd-native",
		Version:              bi.Version,
		HideHelpCommand:      true,
		EnableBashCompletion: true,
		Flags:                []cli.Flag{configFlag},
		Commands: []*cli.Command{
			cmdApply(bi),
			cmdDiff(bi),
			cmdValidate(bi),
			cmdUp(bi),
			cmdDown(bi),
			cmdBounce(bi),
			cmdUpdate(bi),
			cmdStatus(bi),
			cmdLogs(bi),
			cmdShow(bi),
			cmdEdit(bi),
			cmdDoctor(bi),
			cmdServe(bi),
			cmdTUI(bi),
			cmdVersion(bi),
			cmdFork(bi), // hidden, launchd-only
		},
		// Bare `keep` opens the TUI (D14); -h/--help never does (urfave prints
		// help and skips Action).
		Action: func(c *cli.Context) error {
			if c.Args().Len() > 0 {
				return cli.Exit(fmt.Sprintf("unknown command %q", c.Args().First()), 2)
			}
			return runTUI(c, bi)
		},
	}
	return app
}

// configPath resolves the effective Config path from --config or the default.
func configPath(c *cli.Context) string {
	if p := c.String("config"); p != "" {
		return config.ExpandPath(p)
	}
	return config.DefaultConfigPath()
}

// manager loads the Config and builds a Manager, performing opportunistic log
// rotation (D23). Used by every user-facing command (not fork).
func manager(c *cli.Context, bi BuildInfo) (*keep.Manager, error) {
	cfg, err := config.Load(configPath(c))
	if err != nil {
		return nil, err
	}
	mgr, err := keep.NewManager(cfg, bi.Version)
	if err != nil {
		return nil, err
	}
	_, _ = mgr.RotateLogs() // best-effort; never blocks the real command
	return mgr, nil
}
