package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v2"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/keep"
)

func jsonFlag() *cli.BoolFlag {
	return &cli.BoolFlag{Name: "json", Usage: "emit machine-readable JSON"}
}

func cmdVersion(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "print the keep version",
		Action: func(c *cli.Context) error {
			fmt.Fprintf(c.App.Writer, "keep %s (commit %s, built %s)\n", bi.Version, bi.Commit, bi.Date)
			return nil
		},
	}
}

func cmdApply(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "apply",
		Usage: "reconcile live launchd state to the Config",
		Flags: []cli.Flag{jsonFlag()},
		Action: func(c *cli.Context) error {
			mgr, err := manager(c, bi)
			if err != nil {
				return err
			}
			res, err := mgr.Apply()
			if err != nil {
				return err
			}
			if c.Bool("json") {
				return printJSON(c.App.Writer, res)
			}
			printApply(c, res)
			return nil
		},
	}
}

func cmdDiff(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "diff",
		Usage: "preview what apply would change and report drift",
		Flags: []cli.Flag{jsonFlag()},
		Action: func(c *cli.Context) error {
			mgr, err := manager(c, bi)
			if err != nil {
				return err
			}
			plan, err := mgr.ComputePlan()
			if err != nil {
				return err
			}
			if c.Bool("json") {
				return printJSON(c.App.Writer, plan)
			}
			printDiff(c, plan)
			return nil
		},
	}
}

func cmdValidate(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "validate",
		Usage: "check the Config for errors without touching launchd",
		Action: func(c *cli.Context) error {
			// Load performs validation; report and exit non-zero on failure.
			if _, err := config.Load(configPath(c)); err != nil {
				return cli.Exit(err.Error(), 1)
			}
			fmt.Fprintln(c.App.Writer, "config is valid")
			return nil
		},
	}
}

func cmdUp(bi BuildInfo) *cli.Command {
	return verbCommand(bi, "up", "enable and start a service", func(m *keep.Manager, s *config.Service) error {
		return m.Up(s)
	})
}

func cmdDown(bi BuildInfo) *cli.Command {
	return verbCommand(bi, "down", "persistently hold a service down (survives reboot and apply)", func(m *keep.Manager, s *config.Service) error {
		return m.Down(s)
	})
}

func cmdBounce(bi BuildInfo) *cli.Command {
	return verbCommand(bi, "bounce", "restart a running service in place", func(m *keep.Manager, s *config.Service) error {
		return m.Bounce(s)
	})
}

// cmdUpdate runs a Service's declared update commands (docs/prd-update.md):
// Down, run, restore. Exactly one Service per invocation (U2).
func cmdUpdate(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:      "update",
		Usage:     "down a service, run its declared update commands, and restore it",
		ArgsUsage: "<service>",
		Action: func(c *cli.Context) error {
			if c.Args().Len() != 1 {
				return cli.Exit("update requires exactly one service name", 2)
			}
			mgr, err := manager(c, bi)
			if err != nil {
				return err
			}
			targets, err := mgr.Targets([]string{c.Args().First()})
			if err != nil {
				return err
			}
			svc := targets[0]
			// Ctrl-C must reach the update command's process group (it runs
			// in its own), so treat SIGINT/SIGTERM as run cancellation.
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if _, err := mgr.Update(ctx, svc, c.App.Writer); err != nil {
				if errors.Is(err, keep.ErrNoUpdateCommands) || errors.Is(err, keep.ErrUpdateInProgress) {
					return cli.Exit(fmt.Sprintf("update %s: %v", svc.Name, err), 1)
				}
				// The run itself already reported the failure in its output.
				return cli.Exit("", 1)
			}
			return nil
		},
	}
}

func verbCommand(bi BuildInfo, name, usage string, fn func(*keep.Manager, *config.Service) error) *cli.Command {
	return &cli.Command{
		Name:      name,
		Usage:     usage,
		ArgsUsage: "[service...]",
		Action: func(c *cli.Context) error {
			mgr, err := manager(c, bi)
			if err != nil {
				return err
			}
			targets, err := mgr.Targets(c.Args().Slice())
			if err != nil {
				return err
			}
			var firstErr error
			for _, s := range targets {
				if err := fn(mgr, s); err != nil {
					fmt.Fprintf(c.App.ErrWriter, "%s %s: %v\n", name, s.Name, err)
					if firstErr == nil {
						firstErr = err
					}
					continue
				}
				fmt.Fprintf(c.App.Writer, "%s: %s\n", s.Name, name)
			}
			if firstErr != nil {
				return cli.Exit("", 1)
			}
			return nil
		},
	}
}

func cmdStatus(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:      "status",
		Usage:     "show service state from launchd",
		ArgsUsage: "[service...]",
		Flags: []cli.Flag{
			jsonFlag(),
			&cli.BoolFlag{Name: "all", Usage: "show all services (default)"},
		},
		Action: func(c *cli.Context) error {
			mgr, err := manager(c, bi)
			if err != nil {
				return err
			}
			statuses, err := mgr.Status(c.Args().Slice())
			if err != nil {
				return err
			}
			if c.Bool("json") {
				return printJSON(c.App.Writer, statuses)
			}
			printStatus(c, statuses)
			return nil
		},
	}
}

func cmdLogs(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:      "logs",
		Usage:     "tail a service's logs, or all interleaved",
		ArgsUsage: "[service...]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "follow", Aliases: []string{"f"}, Usage: "follow new output"},
			&cli.IntFlag{Name: "lines", Aliases: []string{"n"}, Value: 50, Usage: "lines of history to show"},
		},
		Action: func(c *cli.Context) error {
			mgr, err := manager(c, bi)
			if err != nil {
				return err
			}
			targets, err := mgr.LogTargets(c.Args().Slice())
			if err != nil {
				return err
			}
			// Prefix lines when following more than one stream/service.
			prefix := len(targets) > 1
			if err := mgr.TailOnce(targets, c.Int("lines"), c.App.Writer, prefix); err != nil {
				return err
			}
			if !c.Bool("follow") {
				return nil
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return mgr.Follow(ctx, targets, c.App.Writer, prefix)
		},
	}
}

func cmdShow(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:      "show",
		Usage:     "show a service's resolved command and environment (secrets masked)",
		ArgsUsage: "<service>",
		Flags:     []cli.Flag{jsonFlag()},
		Action: func(c *cli.Context) error {
			if c.Args().Len() != 1 {
				return cli.Exit("show requires exactly one service name", 2)
			}
			mgr, err := manager(c, bi)
			if err != nil {
				return err
			}
			resolved, err := mgr.Show(c.Args().First())
			if err != nil {
				return err
			}
			if c.Bool("json") {
				return printJSON(c.App.Writer, resolved)
			}
			printShow(c, resolved)
			return nil
		},
	}
}

func cmdEdit(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "edit",
		Usage: "open the Config in $EDITOR",
		Action: func(c *cli.Context) error {
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			path := configPath(c)
			cmd := exec.Command(editor, path)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			return cmd.Run()
		},
	}
}

func cmdDoctor(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "read-only health scan with suggested fixes",
		Flags: []cli.Flag{jsonFlag()},
		Action: func(c *cli.Context) error {
			mgr, err := manager(c, bi)
			if err != nil {
				return err
			}
			findings, err := mgr.Doctor()
			if err != nil {
				return err
			}
			if c.Bool("json") {
				if err := printJSON(c.App.Writer, findings); err != nil {
					return err
				}
			} else {
				printDoctor(c, findings)
			}
			if len(findings) > 0 {
				return cli.Exit("", 1)
			}
			return nil
		},
	}
}

func cmdFork(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:      "fork",
		Usage:     "internal launchd launcher (do not run by hand)",
		ArgsUsage: "<service>",
		Hidden:    true, // launchd-only (ADR-0002)
		Action: func(c *cli.Context) error {
			if c.Args().Len() != 1 {
				return cli.Exit("fork requires exactly one service name", 2)
			}
			cfg, err := config.Load(configPath(c))
			if err != nil {
				return err
			}
			mgr, err := keep.NewManager(cfg, bi.Version)
			if err != nil {
				return err
			}
			// On success this never returns (the process image is replaced).
			return mgr.Fork(c.Args().First())
		},
	}
}
