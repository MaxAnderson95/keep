package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/MaxAnderson95/keep/internal/keep"
)

func printApply(c *cli.Context, res keep.ApplyResult) {
	w := c.App.Writer
	report := func(label string, names []string) {
		if len(names) > 0 {
			fmt.Fprintf(w, "%-12s %s\n", label+":", strings.Join(names, ", "))
		}
	}
	report("added", res.Added)
	report("updated", res.Updated)
	report("removed", res.Removed)
	report("held", res.Held)
	report("declared off", res.DeclaredOff)
	report("unchanged", res.Unchanged)
	if len(res.Added)+len(res.Updated)+len(res.Removed) == 0 {
		fmt.Fprintln(w, "everything up to date")
	}
}

func printDiff(c *cli.Context, plan keep.Plan) {
	w := c.App.Writer
	if !plan.HasChanges() && !plan.HasDrift() {
		fmt.Fprintln(w, "no changes; live state matches the Config")
		return
	}
	t := newTable(w)
	t.row("SERVICE", "ACTION", "NOTES")
	for _, s := range plan.Services {
		action := string(s.Kind)
		switch {
		case s.Held:
			action = "hold"
		case s.DeclaredOff && s.Kind == keep.ChangeNoop:
			action = "off"
		}
		t.row(s.Name, action, dash(s.Reason))
	}
	for _, r := range plan.Removes {
		t.row(r.Name, "remove", dash(r.Reason))
	}
	t.flush()
}

func printStatus(c *cli.Context, statuses []keep.ServiceStatus) {
	w := c.App.Writer
	t := newTable(w)
	t.row("SERVICE", "TYPE", "HEALTH", "PID", "UPTIME", "LAST EXIT", "PORT")
	for _, s := range statuses {
		pid := "-"
		if s.PID > 0 {
			pid = strconv.Itoa(s.PID)
		}
		exit := "-"
		if s.LastExit != nil {
			exit = strconv.Itoa(*s.LastExit)
		}
		port := "-"
		if s.Port > 0 {
			if s.PortListening == nil {
				port = strconv.Itoa(s.Port)
			} else if *s.PortListening {
				port = strconv.Itoa(s.Port) + " listening"
			} else {
				port = strconv.Itoa(s.Port) + " NOT listening"
			}
		}
		health := string(s.Health)
		if s.Drift {
			health += " (drift)"
		}
		t.row(s.Name, s.Type, health, pid, dash(s.Uptime), exit, port)
	}
	t.flush()
}

func printShow(c *cli.Context, r keep.Resolved) {
	w := c.App.Writer
	// %-15s fits the widest label (update_timeout:) so values align.
	row := func(label, value string) {
		fmt.Fprintf(w, "%-15s %s\n", label, value)
	}
	row("service:", r.Name)
	row("type:", r.Type)
	row("label:", r.Label)
	row("command:", strings.Join(r.Argv, " "))
	if r.WorkingDir != "" {
		row("working_dir:", r.WorkingDir)
	}
	if r.Umask != "" {
		row("umask:", r.Umask)
	}
	for i, u := range r.Update {
		label := "update:"
		if i > 0 {
			label = ""
		}
		row(label, u)
	}
	if r.UpdateTimeout != "" {
		row("update_timeout:", r.UpdateTimeout)
	}
	if len(r.Env) == 0 {
		fmt.Fprintln(w, "environment: (none contributed by keep)")
		return
	}
	fmt.Fprintln(w, "environment:")
	t := newTable(w)
	t.row("  KEY", "VALUE", "SOURCE")
	for _, e := range r.Env {
		t.row("  "+e.Key, e.Value, e.Source)
	}
	t.flush()
}

func printDoctor(c *cli.Context, findings []keep.Finding) {
	w := c.App.Writer
	if len(findings) == 0 {
		fmt.Fprintln(w, "no problems found")
		return
	}
	for _, f := range findings {
		svc := f.Service
		if svc == "" {
			svc = "-"
		}
		fmt.Fprintf(w, "[%s] %s: %s\n", f.Severity, svc, f.Problem)
		fmt.Fprintf(w, "        fix: %s\n", f.Fix)
	}
	fmt.Fprintf(w, "\n%d problem(s) found\n", len(findings))
}
