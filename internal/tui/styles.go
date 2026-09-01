package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	// Color palette (Makise Kurisu Theme: Crimson, Cyber Cyan, Neon Green, Deep Violet)
	ColorPrimary   = lipgloss.Color("#E63946") // Kurisu Red
	ColorSecondary = lipgloss.Color("#457B9D") // Steins Blue
	ColorAccent    = lipgloss.Color("#00F5D4") // Cyber Cyan
	ColorSuccess   = lipgloss.Color("#52B788") // Neon Green
	ColorWarning   = lipgloss.Color("#F4A261") // Amber Orange
	ColorDanger    = lipgloss.Color("#E76F51") // Alert Red
	ColorMuted     = lipgloss.Color("#6C757D") // Gray
	ColorBgCard    = lipgloss.Color("#161A1D") // Dark Card BG
	ColorBorder    = lipgloss.Color("#343A40") // Border Dark

	// Typography & Container Styles
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			MarginBottom(1)

	TaglineStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)

	BadgeSuccess = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#0B090A")).
			Background(ColorSuccess).
			Padding(0, 1)

	BadgeCached = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#0B090A")).
			Background(ColorAccent).
			Padding(0, 1)

	BadgeError = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(ColorDanger).
			Padding(0, 1)

	CardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(0, 1).
			MarginRight(1)

	CardTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorSecondary)

	CardValue = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			MarginTop(0)

	TableHead = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorAccent).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(ColorBorder)

	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			MarginTop(1)
)

const BannerASCII = `
  _  __  _   _   ____    ___   ____    _   _ 
 | |/ / | | | | |  _ \  |_ _| / ___|  | | | |
 | ' /  | | | | | |_) |  | |  \___ \  | | | |
 | . \  | |_| | |  _ <   | |   ___) | | |_| |
 |_|\_\  \___/  |_| \_\ |___| |____/   \___/  [Universal AI Gateway]
`
