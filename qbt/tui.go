package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

func newTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Interactive torrent table with stop/delete controls",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}
}

// runTUI builds the cluster and runs the interactive table. It is the default
// action when qbt is invoked with no subcommand.
func runTUI() error {
	cl, err := buildCluster()
	if err != nil {
		return err
	}
	p := tea.NewProgram(newTUIModel(cl), tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// --- messages ---

type torrentsMsg struct {
	torrents []AnnotatedTorrent
	warnings []string
}

type actionDoneMsg struct {
	verb     string
	err      error
	warnings []string
}

// refreshTickMsg triggers a follow-up reload shortly after an action, giving the
// server time to apply the state change (resume/pause/delete).
type refreshTickMsg struct{}

func delayedRefresh() tea.Cmd {
	return tea.Tick(600*time.Millisecond, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

// pendingAction holds a stop/delete awaiting confirmation.
type pendingAction struct {
	verb        string // "stop" or "delete"
	hash        string
	name        string
	instance    string
	deleteFiles bool
}

type tuiModel struct {
	cluster   *Cluster
	table     table.Model
	all       []AnnotatedTorrent // full set from the cluster
	torrents  []AnnotatedTorrent // filtered view, parallel to table rows
	pending   *pendingAction
	query     string // active /filter text
	searching bool   // typing in the / prompt
	status    string
	loading   bool
	width     int
	height    int
}

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	statusStyle  = lipgloss.NewStyle().Padding(0, 1)
	confirmStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203")).Padding(0, 1)
	helpStyle    = lipgloss.NewStyle().Faint(true).Padding(0, 1)
)

func newTUIModel(cl *Cluster) tuiModel {
	t := table.New(
		table.WithColumns(buildColumns(0)),
		table.WithFocused(true),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.Bold(true).BorderBottom(true)
	s.Selected = s.Selected.Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("39"))
	t.SetStyles(s)

	return tuiModel{cluster: cl, table: t, loading: true, status: "loading…"}
}

func (m tuiModel) Init() tea.Cmd {
	return m.loadCmd()
}

// loadCmd fetches torrents from all instances, capturing per-instance warnings.
func (m tuiModel) loadCmd() tea.Cmd {
	cl := m.cluster
	return func() tea.Msg {
		var ts []AnnotatedTorrent
		warnings := captureWarnings(func() {
			ts, _ = cl.ListAll(context.Background(), qbt.TorrentFilterOptions{})
		})
		sort.Slice(ts, func(i, j int) bool {
			if ts[i].Instance != ts[j].Instance {
				return ts[i].Instance < ts[j].Instance
			}
			return ts[i].Name < ts[j].Name
		})
		return torrentsMsg{torrents: ts, warnings: warnings}
	}
}

func actionCmd(cl *Cluster, p pendingAction) tea.Cmd {
	return func() tea.Msg {
		var err error
		warnings := captureWarnings(func() {
			ctx := context.Background()
			switch p.verb {
			case "delete":
				err = cl.Delete(ctx, []string{p.hash}, p.deleteFiles)
			case "start":
				err = cl.Resume(ctx, []string{p.hash})
			default: // stop
				err = cl.Pause(ctx, []string{p.hash})
			}
		})
		return actionDoneMsg{verb: p.verb, err: err, warnings: warnings}
	}
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.table.SetColumns(buildColumns(msg.Width))
		m.table.SetWidth(msg.Width)
		// Chrome around the table (table.SetHeight already counts its own header):
		// title(1) + status(1) + help(1) = 3 lines.
		m.table.SetHeight(max(3, msg.Height-3))
		return m, nil

	case torrentsMsg:
		m.loading = false
		m.all = msg.torrents
		m.applyFilter()
		m.status = fmt.Sprintf("%d torrents", len(m.torrents)) + filterSuffix(m.query) + warnSuffix(msg.warnings)
		return m, nil

	case actionDoneMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("%s failed: %v", msg.verb, msg.err)
			return m, m.loadCmd()
		}
		m.status = msg.verb + " ok" + warnSuffix(msg.warnings)
		// Refresh now and again shortly after, so the new state reliably shows.
		return m, tea.Batch(m.loadCmd(), delayedRefresh())

	case refreshTickMsg:
		return m, m.loadCmd()

	case tea.KeyMsg:
		switch {
		case m.pending != nil:
			return m.updateConfirm(msg)
		case m.searching:
			return m.updateSearch(msg)
		default:
			return m.updateNormal(msg)
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// updateConfirm handles keys while a stop/delete confirmation is showing.
func (m tuiModel) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		p := *m.pending
		m.pending = nil
		m.status = p.verb + "ing…"
		return m, actionCmd(m.cluster, p)
	default: // n, esc, anything else cancels
		m.pending = nil
		m.status = "cancelled"
		return m, nil
	}
}

// updateNormal handles keys in the table view.
func (m tuiModel) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "r":
		m.status = "refreshing…"
		return m, m.loadCmd()
	case "/":
		m.searching = true
		return m, nil
	case "p":
		// start/resume — non-destructive, no confirm.
		if t, ok := m.selected(); ok {
			m.status = "starting…"
			return m, actionCmd(m.cluster, pendingAction{verb: "start", hash: t.Hash})
		}
		return m, nil
	case "s":
		if t, ok := m.selected(); ok {
			m.pending = &pendingAction{verb: "stop", hash: t.Hash, name: t.Name, instance: t.Instance}
		}
		return m, nil
	case "d":
		if t, ok := m.selected(); ok {
			m.pending = &pendingAction{verb: "delete", hash: t.Hash, name: t.Name, instance: t.Instance}
		}
		return m, nil
	case "D":
		if t, ok := m.selected(); ok {
			m.pending = &pendingAction{verb: "delete", hash: t.Hash, name: t.Name, instance: t.Instance, deleteFiles: true}
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// updateSearch handles keys while typing in the / filter prompt. Filtering is
// incremental — the table updates on every keystroke (vim-like).
func (m tuiModel) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc": // cancel: drop the filter
		m.searching = false
		m.query = ""
		m.applyFilter()
		m.status = fmt.Sprintf("%d torrents", len(m.torrents))
	case "enter": // confirm: keep the filter, leave search mode
		m.searching = false
		m.status = fmt.Sprintf("%d torrents", len(m.torrents)) + filterSuffix(m.query)
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.applyFilter()
		}
	case "ctrl+c":
		return m, tea.Quit
	default:
		if msg.Type == tea.KeyRunes {
			m.query += string(msg.Runes)
			m.applyFilter()
		}
	}
	return m, nil
}

// applyFilter rebuilds the visible rows from m.all using the current query
// (case-insensitive substring match on name or instance).
func (m *tuiModel) applyFilter() {
	q := strings.ToLower(m.query)
	var out []AnnotatedTorrent
	for _, t := range m.all {
		if q == "" ||
			strings.Contains(strings.ToLower(t.Name), q) ||
			strings.Contains(strings.ToLower(t.Instance), q) {
			out = append(out, t)
		}
	}
	m.torrents = out
	m.table.SetRows(torrentRows(out))
	if m.table.Cursor() >= len(out) {
		m.table.SetCursor(max(0, len(out)-1))
	}
}

func filterSuffix(query string) string {
	if query == "" {
		return ""
	}
	return "  · filter:/" + query
}

func (m tuiModel) selected() (AnnotatedTorrent, bool) {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.torrents) {
		return AnnotatedTorrent{}, false
	}
	return m.torrents[i], true
}

func (m tuiModel) View() string {
	title := titleStyle.Render("qbt — " + fmt.Sprintf("%d instance(s)", len(m.cluster.nodes)))

	var footer string
	switch {
	case m.pending != nil:
		p := m.pending
		q := fmt.Sprintf("%s %q on %q?", p.verb, truncate(p.name, 40), p.instance)
		if p.deleteFiles {
			q = fmt.Sprintf("delete %q on %q WITH FILES?", truncate(p.name, 40), p.instance)
		}
		footer = confirmStyle.Render(q + "  (y/n)")
	case m.searching:
		footer = statusStyle.Render("/" + m.query + "█")
	default:
		footer = statusStyle.Render(m.status)
	}

	help := helpStyle.Render("↑/↓ move · / search · p start · s stop · d delete · D delete+files · r refresh · q quit")

	return lipgloss.JoinVertical(lipgloss.Left, title, m.table.View(), footer, help)
}

// --- helpers ---

func buildColumns(width int) []table.Column {
	// Fixed widths for everything but NAME, which flexes to fill.
	const (
		inst  = 10
		state = 14
		prog  = 6
		size  = 10
		dl    = 11
		up    = 11
		ratio = 6
	)
	fixed := inst + state + prog + size + dl + up + ratio
	name := width - fixed - 18 // padding/borders
	if name < 20 {
		name = 20
	}
	return []table.Column{
		{Title: "INSTANCE", Width: inst},
		{Title: "NAME", Width: name},
		{Title: "STATE", Width: state},
		{Title: "PROG", Width: prog},
		{Title: "SIZE", Width: size},
		{Title: "DL", Width: dl},
		{Title: "UP", Width: up},
		{Title: "RATIO", Width: ratio},
	}
}

func torrentRows(torrents []AnnotatedTorrent) []table.Row {
	rows := make([]table.Row, len(torrents))
	for i, t := range torrents {
		rows[i] = table.Row{
			t.Instance,
			t.Name,
			string(t.State),
			fmt.Sprintf("%.0f%%", t.Progress*100),
			humanize.IBytes(uint64(t.Size)),
			humanize.IBytes(uint64(t.DlSpeed)) + "/s",
			humanize.IBytes(uint64(t.UpSpeed)) + "/s",
			fmt.Sprintf("%.2f", t.Ratio),
		}
	}
	return rows
}

// captureWarnings runs fn with warnf redirected into a slice. fn must join any
// goroutines it spawns before returning (the cluster methods do).
func captureWarnings(fn func()) []string {
	var mu sync.Mutex
	var out []string
	warnSink = func(s string) {
		mu.Lock()
		out = append(out, s)
		mu.Unlock()
	}
	fn()
	warnSink = nil
	return out
}

func warnSuffix(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	return fmt.Sprintf("  ⚠ %d warning(s): %s", len(warnings), warnings[0])
}
