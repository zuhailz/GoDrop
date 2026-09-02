package styles

import (
	"github.com/charmbracelet/lipgloss"
)

const (
	Primary   = "#00D9FF"
	Success   = "#00FF88"
	Error     = "#FF4444"
	Warning   = "#FFB454"
	Text      = "#E6E6E6"
	Muted     = "#8A8A8A"
	Dim       = "#56566E"
	PanelBg   = "#131320"
	AppBg     = "#0B0B15"
	PanelEdge = "#2C2C4A"
)

var (
	AppStyle = lipgloss.NewStyle().
			Padding(1, 2)

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(Primary))

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Muted))

	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(AppBg)).
			Background(lipgloss.Color(Primary)).
			Padding(0, 1)

	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(PanelEdge)).
			Background(lipgloss.Color(PanelBg)).
			Padding(0, 1)

	PanelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(Primary))

	DividerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Dim))

	InfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Muted))

	SuccessStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Success))

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Error))

	WarningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Warning))

	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(Primary)).
			Padding(0, 1)

	ProgressBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(Primary))

	ProgressBackgroundStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(Dim))

	SelectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(Primary)).
				Bold(true)

	NormalItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Text))

	PeerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Primary))

	TransferIDStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Warning))

	FilenameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Text)).
			Bold(true)

	StatusWaitingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(Warning))

	StatusAcceptedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(Success))

	StatusInProgressStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(Primary))

	StatusCompletedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(Success))

	StatusFailedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(Error))

	HelpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Muted))
)
