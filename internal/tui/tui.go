// Package tui is the bubbletea dashboard (D14). Bare `keep` and `keep tui` open
// it; it consumes the same status layer the CLI exposes and drives up/down/
// bounce plus a log viewer.
package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/keep"
)

// Run starts the TUI against a Manager.
func Run(mgr *keep.Manager) error {
	p := tea.NewProgram(newModel(mgr), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type mode int

const (
	modeList mode = iota
	modeLogs
)

type model struct {
	mgr      *keep.Manager
	statuses []keep.ServiceStatus
	cursor   int
	mode     mode
	logLines []string
	logFor   string
	err      error
	width    int
	height   int
}

func newModel(mgr *keep.Manager) model {
	return model{mgr: mgr}
}

type statusMsg []keep.ServiceStatus
type logsMsg struct {
	service string
	lines   []string
}
type errMsg struct{ err error }
type tickMsg time.Time

func (m model) Init() tea.Cmd {
	return tea.Batch(refreshCmd(m.mgr), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func refreshCmd(mgr *keep.Manager) tea.Cmd {
	return func() tea.Msg {
		st, err := mgr.Status(nil)
		if err != nil {
			return errMsg{err}
		}
		return statusMsg(st)
	}
}

func logsCmd(mgr *keep.Manager, service string) tea.Cmd {
	return func() tea.Msg {
		targets, err := mgr.LogTargets([]string{service})
		if err != nil {
			return errMsg{err}
		}
		var buf lineBuffer
		_ = mgr.TailOnce(targets, 200, &buf, true)
		return logsMsg{service: service, lines: buf.lines}
	}
}

func (m model) selected() (keep.ServiceStatus, bool) {
	if m.cursor < 0 || m.cursor >= len(m.statuses) {
		return keep.ServiceStatus{}, false
	}
	return m.statuses[m.cursor], true
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case statusMsg:
		m.statuses = msg
		if m.cursor >= len(m.statuses) {
			m.cursor = max(0, len(m.statuses)-1)
		}
		return m, nil

	case logsMsg:
		if msg.service == m.logFor {
			m.logLines = msg.lines
		}
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tickCmd(), refreshCmd(m.mgr)}
		if m.mode == modeLogs && m.logFor != "" {
			cmds = append(cmds, logsCmd(m.mgr, m.logFor))
		}
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		if m.mode == modeLogs {
			m.mode = modeList
			return m, nil
		}
		return m, tea.Quit
	case "esc":
		if m.mode == modeLogs {
			m.mode = modeList
		}
		return m, nil
	case "up", "k":
		if m.mode == modeList && m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down", "j":
		if m.mode == modeList && m.cursor < len(m.statuses)-1 {
			m.cursor++
		}
		return m, nil
	case "r":
		return m, refreshCmd(m.mgr)
	case "u":
		return m.verb(m.mgr.Up)
	case "d":
		return m.verb(m.mgr.Down)
	case "b":
		return m.verb(m.mgr.Bounce)
	case "l", "enter":
		if sel, ok := m.selected(); ok {
			m.mode = modeLogs
			m.logFor = sel.Name
			m.logLines = nil
			return m, logsCmd(m.mgr, sel.Name)
		}
	}
	return m, nil
}

func (m model) verb(run func(*config.Service) error) (tea.Model, tea.Cmd) {
	sel, ok := m.selected()
	if !ok {
		return m, nil
	}
	name := sel.Name
	return m, tea.Sequence(
		func() tea.Msg {
			if err := m.runOn(name, run); err != nil {
				return errMsg{err}
			}
			return nil
		},
		refreshCmd(m.mgr),
	)
}

func (m model) runOn(name string, run func(*config.Service) error) error {
	targets, err := m.mgr.Targets([]string{name})
	if err != nil || len(targets) == 0 {
		return err
	}
	return run(targets[0])
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("63"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

func healthColor(h keep.Health) lipgloss.Style {
	switch h {
	case keep.HealthRunning, keep.HealthIdle:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	case keep.HealthHeld, keep.HealthDeclaredOff:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	case keep.HealthUpdating:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	case keep.HealthError, keep.HealthStopped, keep.HealthNotLoaded:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	default:
		return lipgloss.NewStyle()
	}
}

func (m model) View() string {
	if m.mode == modeLogs {
		return m.logsView()
	}
	return m.listView()
}

func (m model) listView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("keep — services"))
	b.WriteString("\n\n")

	header := fmt.Sprintf("  %-18s %-10s %-14s %-8s %s", "SERVICE", "TYPE", "HEALTH", "PID", "UPTIME")
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	if len(m.statuses) == 0 {
		b.WriteString(dimStyle.Render("  (no services declared)"))
		b.WriteString("\n")
	}
	for i, s := range m.statuses {
		pid := "-"
		if s.PID > 0 {
			pid = strconv.Itoa(s.PID)
		}
		health := string(s.Health)
		if s.Drift {
			health += "*"
		}
		line := fmt.Sprintf("  %-18s %-10s %-14s %-8s %s",
			truncate(s.Name, 18), truncate(s.Type, 10),
			truncate(health, 14), pid, dashTUI(s.Uptime))
		if i == m.cursor {
			b.WriteString(selStyle.Render(truncate(strings.TrimPrefix(line, "  "), m.contentWidth())))
		} else {
			b.WriteString(healthColor(s.Health).Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.err != nil {
		b.WriteString(errStyle.Render("error: " + m.err.Error()))
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("↑/↓ move • u up • d down • b bounce • l logs • r refresh • q quit"))
	return b.String()
}

func (m model) logsView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("keep — logs: " + m.logFor))
	b.WriteString("\n\n")
	if len(m.logLines) == 0 {
		b.WriteString(dimStyle.Render("(no log output yet)"))
		b.WriteString("\n")
	}
	start := 0
	maxLines := m.height - 6
	if maxLines > 0 && len(m.logLines) > maxLines {
		start = len(m.logLines) - maxLines
	}
	for _, line := range m.logLines[start:] {
		b.WriteString(truncate(line, m.contentWidth()))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("esc/q back • r refresh"))
	return b.String()
}

func (m model) contentWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

type lineBuffer struct{ lines []string }

func (b *lineBuffer) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			b.lines = append(b.lines, line)
		}
	}
	return len(p), nil
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func dashTUI(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
