package banner

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	baseMargin  = 10
	motionChars = 6
)

const Art = `  ____       ____                  
 / ___| ___ |  _ \ _ __ ___  _ __  
| |  _ / _ \| | | | '__/ _ \| '_ \ 
| |_| | (_) | |_| | | | (_) | |_) |
 \____|\___/|____/|_|  \___/| .__/ 
                            |_|    `

func Render() string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#00D9FF")).
		Render(Art)
}

func RenderFlying(progress float64) string {
	if progress >= 1 {
		return Render()
	}
	if progress < 0 {
		progress = 0
	}

	raw := strings.Split(strings.TrimRight(Art, "\n"), "\n")
	maxArt := 0
	for _, l := range raw {
		if len(l) > maxArt {
			maxArt = len(l)
		}
	}

	p := 1 - progress
	slide := -int(math.Round(p * 12))
	shake := int(math.Round(math.Sin(progress*math.Pi*20) * 3.0 * p))

	leftPad := baseMargin + slide + shake
	if leftPad < motionChars {
		leftPad = motionChars
	}
	rightPad := 2*baseMargin - leftPad

	lines := make([]string, 0, len(raw))
	for i, line := range raw {
		wave := 0.6 + 0.4*math.Sin(progress*math.Pi*20+float64(i)*0.8)
		streak := int(math.Round(p * float64(2+(i%3)*2) * wave))
		if streak > leftPad {
			streak = leftPad
		}
		if streak < 0 {
			streak = 0
		}

		padded := line + strings.Repeat(" ", maxArt-len(line))
		lines = append(lines,
			strings.Repeat(" ", leftPad-streak)+
				strings.Repeat("═", streak)+
				padded+
				strings.Repeat(" ", rightPad))
	}

	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#00D9FF")).
		Render(strings.Join(lines, "\n"))
}
