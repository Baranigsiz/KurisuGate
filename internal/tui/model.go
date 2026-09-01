package tui

import (
	"time"

	"github.com/Baranigsiz/kurisu/internal/metrics"
	tea "github.com/charmbracelet/bubbletea"
)

type tickMsg time.Time

// Model holds the interactive TUI state
type Model struct {
	collector *metrics.Collector
	snapshot  metrics.Snapshot
	width     int
	height    int
	quitting  bool
}

// NewModel constructs a new Bubbletea model
func NewModel(collector *metrics.Collector) Model {
	return Model{
		collector: collector,
		snapshot:  collector.GetSnapshot(),
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Init() tea.Cmd {
	return tickCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "r":
			m.snapshot = m.collector.GetSnapshot()
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.snapshot = m.collector.GetSnapshot()
		return m, tickCmd()
	}

	return m, nil
}
