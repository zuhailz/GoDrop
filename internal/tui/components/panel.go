package components

import (
	"strings"

	"github.com/zuhailz/GoDrop/internal/tui/styles"
)

func Panel(title, body string, width int) string {
	style := styles.PanelStyle
	if width > 0 {
		style = style.Width(width - 2)
	}

	body = strings.TrimRight(body, "\n")

	if title == "" {
		return style.Render(body)
	}

	dividerWidth := width - 4
	if dividerWidth < 0 {
		dividerWidth = 0
	}

	content := styles.PanelTitleStyle.Render(title) + "\n" +
		styles.DividerStyle.Render(strings.Repeat("─", dividerWidth)) + "\n\n" +
		body

	return style.Render(content)
}
