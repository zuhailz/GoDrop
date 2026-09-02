package components

import (
	"fmt"
	"strings"

	"github.com/zuhailz/GoDrop/internal/tui/styles"
)

func ProgressBar(width int, percent float64) string {
	if width < 3 {
		return ""
	}

	filled := int(float64(width-2) * percent)
	if filled < 0 {
		filled = 0
	}
	if filled > width-2 {
		filled = width - 2
	}

	empty := width - 2 - filled

	return "[" +
		styles.ProgressBarStyle.Render(strings.Repeat("█", filled)) +
		styles.ProgressBackgroundStyle.Render(strings.Repeat("░", empty)) +
		"]"
}

func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func FormatProgress(offset, total int64) string {
	return FormatProgressWithWidth(offset, total, 20)
}

func FormatProgressWithWidth(offset, total int64, width int) string {
	percent := float64(0)
	if total > 0 {
		percent = float64(offset) / float64(total)
	}
	return fmt.Sprintf("%s %s",
		ProgressBar(width, percent),
		fmt.Sprintf("%.0f%% (%s / %s)", percent*100, FormatBytes(offset), FormatBytes(total)),
	)
}
