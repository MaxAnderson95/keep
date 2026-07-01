package cli

import (
	"github.com/urfave/cli/v2"

	"github.com/MaxAnderson95/keep/internal/tui"
)

func cmdTUI(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "tui",
		Usage: "open the interactive dashboard",
		Action: func(c *cli.Context) error {
			return runTUI(c, bi)
		},
	}
}

func runTUI(c *cli.Context, bi BuildInfo) error {
	mgr, err := manager(c, bi)
	if err != nil {
		return err
	}
	return tui.Run(mgr)
}
