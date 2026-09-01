package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	if m.quitting {
		return "\n  ✨ Kurisu Gateway Dashboard closed.\n\n"
	}

	snap := m.snapshot

	// 1. Header & Status Bar
	statusBadge := BadgeSuccess.Render("● ONLINE")
	uptimeStr := fmt.Sprintf("Uptime: %s", snap.Uptime.Round(1000000000).String())
	headerInfo := lipgloss.JoinHorizontal(lipgloss.Center, statusBadge, "  ", TaglineStyle.Render(uptimeStr))

	header := lipgloss.JoinVertical(
		lipgloss.Left,
		TitleStyle.Render(BannerASCII),
		headerInfo,
		"",
	)

	// 2. Metrics Cards
	var successRate float64 = 100.0
	if snap.TotalRequests > 0 {
		successRate = (float64(snap.SuccessRequests) / float64(snap.TotalRequests)) * 100.0
	}

	card1 := CardStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			CardTitle.Render("📊 REQUESTS"),
			CardValue.Render(fmt.Sprintf("%d", snap.TotalRequests)),
			TaglineStyle.Render(fmt.Sprintf("Success: %.1f%% (%d)", successRate, snap.SuccessRequests)),
			TaglineStyle.Render(fmt.Sprintf("Failed: %d", snap.FailedRequests)),
		),
	)

	card2 := CardStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			CardTitle.Render("⚡ CACHE ENGINE"),
			CardValue.Render(fmt.Sprintf("%.1f%% Hit Rate", snap.CacheHitRatio)),
			TaglineStyle.Render(fmt.Sprintf("Exact Hits: %d", snap.ExactCacheHits)),
			TaglineStyle.Render(fmt.Sprintf("Semantic Hits: %d", snap.SemanticCacheHits)),
		),
	)

	card3 := CardStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			CardTitle.Render("💰 COST SAVINGS"),
			CardValue.Render(fmt.Sprintf("$%.4f Saved", snap.TotalCostSaved)),
			TaglineStyle.Render(fmt.Sprintf("Incurred: $%.4f", snap.TotalCostIncurred)),
			TaglineStyle.Render(fmt.Sprintf("Tokens: %d", snap.TotalTokens)),
		),
	)

	card4 := CardStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			CardTitle.Render("⏱️ LATENCY"),
			CardValue.Render(fmt.Sprintf("%.1f ms Avg", snap.AvgLatencyMs)),
			TaglineStyle.Render(fmt.Sprintf("Active Providers: %d", len(snap.ProviderCounts))),
			TaglineStyle.Render(fmt.Sprintf("Models Routed: %d", len(snap.ModelCounts))),
		),
	)

	cardsRow := lipgloss.JoinHorizontal(lipgloss.Top, card1, card2, card3, card4)

	// 3. Live Recent Requests Table
	tableHeader := TableHead.Render(
		fmt.Sprintf("  %-8s %-8s %-24s %-16s %-12s %-10s %-10s",
			"TIME", "STATUS", "MODEL", "PROVIDER", "CACHE", "LATENCY", "SAVED",
		),
	)

	var logRows []string
	if len(snap.RecentLogs) == 0 {
		logRows = append(logRows, TaglineStyle.Render("  No requests recorded yet. Waiting for incoming traffic..."))
	} else {
		// Show most recent 8 items in reverse
		count := 0
		for i := len(snap.RecentLogs) - 1; i >= 0 && count < 8; i-- {
			log := snap.RecentLogs[i]
			count++

			timeStr := log.Timestamp.Format("15:04:05")

			statusStr := fmt.Sprintf("%d", log.StatusCode)
			if log.StatusCode >= 200 && log.StatusCode < 300 {
				statusStr = lipgloss.NewStyle().Foreground(ColorSuccess).Render(statusStr)
			} else {
				statusStr = lipgloss.NewStyle().Foreground(ColorDanger).Render(statusStr)
			}

			cacheBadge := "-"
			if log.Cached {
				if log.CacheType == "semantic" {
					cacheBadge = lipgloss.NewStyle().Foreground(ColorAccent).Render("SEMANTIC")
				} else {
					cacheBadge = lipgloss.NewStyle().Foreground(ColorSuccess).Render("EXACT")
				}
			}

			modelName := log.Model
			if len(modelName) > 22 {
				modelName = modelName[:20] + ".."
			}

			costSavedStr := "-"
			if log.CostSaved > 0 {
				costSavedStr = fmt.Sprintf("+$%.4f", log.CostSaved)
			}

			row := fmt.Sprintf("  %-8s %-8s %-24s %-16s %-12s %-10s %-10s",
				timeStr,
				statusStr,
				modelName,
				log.Provider,
				cacheBadge,
				log.Duration.Round(100000).String(),
				costSavedStr,
			)
			logRows = append(logRows, row)
		}
	}

	tableSection := lipgloss.JoinVertical(
		lipgloss.Left,
		"",
		lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render("📡 REAL-TIME TRAFFIC & EVENT LOGS"),
		tableHeader,
		strings.Join(logRows, "\n"),
	)

	// 4. Footer
	footer := HelpStyle.Render("  [q] Quit Dashboard  •  [r] Manual Refresh  •  Docs: github.com/Baranigsiz/KurisuGate")

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		cardsRow,
		tableSection,
		"",
		footer,
	)
}
