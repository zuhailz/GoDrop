package receiver

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zuhailz/GoDrop/internal/banner"
	"github.com/zuhailz/GoDrop/internal/protocol"
	"github.com/zuhailz/GoDrop/internal/receiver"
	"github.com/zuhailz/GoDrop/internal/transfer"
	"github.com/zuhailz/GoDrop/internal/tui/components"
	"github.com/zuhailz/GoDrop/internal/tui/styles"
)

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Accept key.Binding
	Reject key.Binding
	CopyID key.Binding
	Quit   key.Binding
	FeedUp key.Binding
	FeedDn key.Binding
}

var keys = keyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Accept: key.NewBinding(key.WithKeys("a", "enter"), key.WithHelp("a/enter", "accept")),
	Reject: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reject")),
	CopyID: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy room key")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	FeedUp: key.NewBinding(key.WithKeys("["), key.WithHelp("[", "feed older")),
	FeedDn: key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "feed newer")),
}

type ProgressUpdate struct {
	Progress transfer.PeerProgress
}

type OfferReceived struct {
	Offer receiver.FileOffer
}

type SystemEventMsg struct {
	Event protocol.SystemEvent
}

type connectedMsg struct {
	err error
}

type splashTickMsg struct{}

type model struct {
	receiver    *receiver.Receiver
	offers      []receiver.FileOffer
	transfers   []transfer.PeerProgress
	feed        []components.FeedItem
	selectedIdx int
	saveDir     string
	width       int
	height      int
	err         error
	feedIdx     int

	roomKey        string
	connectIP      string
	connectErr     error
	connected      bool
	splash         bool
	splashProgress float64

	transferNames map[string]string
}

func NewModel(r *receiver.Receiver, saveDir, roomKey, connectIP string) model {
	return model{
		receiver:      r,
		saveDir:       saveDir,
		roomKey:       roomKey,
		connectIP:     connectIP,
		splash:        true,
		offers:        make([]receiver.FileOffer, 0),
		transfers:     make([]transfer.PeerProgress, 0),
		feed:          make([]components.FeedItem, 0),
		transferNames: make(map[string]string),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.connect(),
		tickSplash(),
		waitForProgress(m.receiver.ProgressChan()),
		waitForOffer(m.receiver.OfferChan()),
		waitForSystemEvent(m.receiver.SystemEventChan()),
	)
}

func waitForSystemEvent(ch <-chan protocol.SystemEvent) tea.Cmd {
	return func() tea.Msg {
		if event, ok := <-ch; ok {
			return SystemEventMsg{Event: event}
		}
		return nil
	}
}

func (m model) connect() tea.Cmd {
	return func() tea.Msg {
		if m.connectIP != "" {
			// Direct IP mode: still need the room key for auth.
			if err := m.receiver.SetRoomKey(m.roomKey); err != nil {
				return connectedMsg{err: err}
			}
			return connectedMsg{err: m.receiver.Connect(m.connectIP)}
		}
		return connectedMsg{err: m.receiver.ConnectByRoomKey(m.roomKey, 10)}
	}
}

func tickSplash() tea.Cmd {
	return tea.Tick(30*time.Millisecond, func(time.Time) tea.Msg {
		return splashTickMsg{}
	})
}

func waitForProgress(ch <-chan transfer.PeerProgress) tea.Cmd {
	return func() tea.Msg {
		if progress, ok := <-ch; ok {
			return ProgressUpdate{Progress: progress}
		}
		return nil
	}
}

func waitForOffer(ch <-chan receiver.FileOffer) tea.Cmd {
	return func() tea.Msg {
		if offer, ok := <-ch; ok {
			return OfferReceived{Offer: offer}
		}
		return nil
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.splash {
			if key.Matches(msg, keys.Quit) {
				return m, tea.Quit
			}
			m.splash = false
			return m, nil
		}
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.CopyID):
			components.CopyToClipboard(m.roomKey)
			m.feed = append(m.feed, components.FeedItem{Event: true, Text: "Copied room key: " + m.roomKey, Kind: components.FeedSuccess})
		case key.Matches(msg, keys.FeedUp):
			if len(m.feed) > components.MaxFeedVisible && m.feedIdx < len(m.feed)-components.MaxFeedVisible {
				m.feedIdx++
			}
		case key.Matches(msg, keys.FeedDn):
			if m.feedIdx > 0 {
				m.feedIdx--
			}
		case key.Matches(msg, keys.Up):
			if m.selectedIdx > 0 {
				m.selectedIdx--
			}
		case key.Matches(msg, keys.Down):
			if m.selectedIdx < len(m.offers)-1 {
				m.selectedIdx++
			}
		case key.Matches(msg, keys.Accept):
			if len(m.offers) > 0 && m.selectedIdx < len(m.offers) {
				offer := m.offers[m.selectedIdx]
				savePath := filepath.Join(m.saveDir, offer.Filename)
				finalPath, renamed, err := m.receiver.AcceptTransfer(offer.TransferID, savePath)
				if err != nil {
					m.err = err
				} else {
					displayName := filepath.Base(finalPath)
					if offer.Folder {
						displayName = strings.TrimSuffix(displayName, ".zip")
					}
					m.transferNames[offer.TransferID] = displayName
					if renamed {
						m.feed = append(m.feed, components.FeedItem{
							Event: true,
							Text:  fmt.Sprintf("%s already exists, saving as %s", offer.Filename, displayName),
							Kind:  components.FeedWarning,
						})
					}
					m.offers = append(m.offers[:m.selectedIdx], m.offers[m.selectedIdx+1:]...)
					if m.selectedIdx >= len(m.offers) && m.selectedIdx > 0 {
						m.selectedIdx--
					}
				}
			}
		case key.Matches(msg, keys.Reject):
			if len(m.offers) > 0 && m.selectedIdx < len(m.offers) {
				offer := m.offers[m.selectedIdx]
				if err := m.receiver.RejectTransfer(offer.TransferID); err != nil {
					m.err = err
				} else {
					m.offers = append(m.offers[:m.selectedIdx], m.offers[m.selectedIdx+1:]...)
					if m.selectedIdx >= len(m.offers) && m.selectedIdx > 0 {
						m.selectedIdx--
					}
				}
			}
		}

	case tea.MouseMsg:
		if m.splash {
			return m, nil
		}
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelUp {
			if m.feedIdx < len(m.feed)-components.MaxFeedVisible {
				m.feedIdx++
			}
		} else if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelDown {
			if m.feedIdx > 0 {
				m.feedIdx--
			}
		}

	case connectedMsg:
		if msg.err != nil {
			m.connectErr = msg.err
			m.splash = false
			return m, tea.Quit
		}
		m.connected = true
		m.feed = append(m.feed, components.FeedItem{
			Event: true,
			Text:  "connected to room",
			Kind:  components.FeedSuccess,
		})
		if m.splashProgress >= 1.0 {
			m.splash = false
		}
		return m, nil

	case splashTickMsg:
		m.splashProgress += 0.01
		if m.splashProgress >= 1.0 {
			m.splashProgress = 1.0
			m.splash = false
			return m, nil
		}
		return m, tickSplash()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case ProgressUpdate:
		found := false
		for i, t := range m.transfers {
			if t.TransferID == msg.Progress.TransferID {
				m.transfers[i] = msg.Progress
				found = true
				break
			}
		}
		if !found {
			m.transfers = append(m.transfers, msg.Progress)
		}
		m.offers = m.receiver.PendingOffers()
		return m, tea.Batch(
			waitForProgress(m.receiver.ProgressChan()),
			waitForOffer(m.receiver.OfferChan()),
		)

	case OfferReceived:
		m.offers = m.receiver.PendingOffers()
		return m, waitForOffer(m.receiver.OfferChan())

	case SystemEventMsg:
		m.feed = append(m.feed, components.FeedItem{Event: true, Text: msg.Event.Text, Kind: components.FeedNeutral})
		return m, waitForSystemEvent(m.receiver.SystemEventChan())
	}

	m.offers = m.receiver.PendingOffers()
	return m, nil
}

func (m model) View() string {
	innerWidth := m.width - 4
	if innerWidth < 20 {
		innerWidth = 20
	}
	barWidth := innerWidth - 40
	if barWidth < 10 {
		barWidth = 10
	}
	if barWidth > 36 {
		barWidth = 36
	}

	var b strings.Builder

	if m.splash {
		return m.renderSplash(innerWidth)
	}

	b.WriteString(styles.TitleStyle.Render("GoDrop Receiver"))
	b.WriteString("  ")
	b.WriteString(styles.SubtitleStyle.Render(fmt.Sprintf("connected to %s", m.receiver.RoomKey)))
	b.WriteString("\n")
	b.WriteString(styles.InfoStyle.Render("receiving files to " + m.saveDir))
	b.WriteString("\n\n")

	var offersBody strings.Builder
	if len(m.offers) == 0 {
		offersBody.WriteString(styles.InfoStyle.Render("  No pending offers. Waiting for the host to send something."))
	} else {
		for i, offer := range m.offers {
			style := styles.NormalItemStyle
			cursor := "  "
			if i == m.selectedIdx {
				style = styles.SelectedItemStyle
				cursor = "▸ "
			}
			offersBody.WriteString(style.Render(fmt.Sprintf("%s%s  (%s)  from  %s",
				cursor,
				offer.Filename,
				components.FormatBytes(offer.FileSize),
				offer.Sender,
			)))
			offersBody.WriteString("\n")
		}
	}
	b.WriteString(components.Panel(fmt.Sprintf("PENDING OFFERS (%d)", len(m.offers)), offersBody.String(), innerWidth))
	b.WriteString("\n\n")

	var transfersBody strings.Builder
	if len(m.transfers) == 0 {
		transfersBody.WriteString(styles.InfoStyle.Render("  No active transfers"))
	} else {
		for _, t := range m.transfers {
			transfersBody.WriteString(m.renderTransferRow(t, barWidth))
			transfersBody.WriteString("\n")
		}
	}
	b.WriteString(components.Panel("ACTIVE TRANSFERS", transfersBody.String(), innerWidth))
	b.WriteString("\n\n")

	var feedBody strings.Builder
	if len(m.feed) == 0 {
		feedBody.WriteString(styles.InfoStyle.Render("  No activity yet. Waiting for the host."))
	} else {
		start := len(m.feed) - components.MaxFeedVisible - m.feedIdx
		if start < 0 {
			start = 0
		}
		end := len(m.feed) - m.feedIdx
		if end < start {
			end = start
		}
		for i := start; i < end; i++ {
			item := m.feed[i]
			feedBody.WriteString(components.RenderFeedEvent(item))
		}
		if start > 0 {
			feedBody.WriteString(styles.InfoStyle.Render(fmt.Sprintf("  ▾ %d older…", start)))
		}
	}
	b.WriteString(components.Panel(fmt.Sprintf("FEED (%d)", len(m.feed)), feedBody.String(), innerWidth))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(styles.ErrorStyle.Render(fmt.Sprintf("  Error: %v", m.err)))
		b.WriteString("\n\n")
	}

	b.WriteString(styles.HelpStyle.Render("↑/↓: navigate  •  a/enter: accept  •  r: reject  •  c: copy room key  •  q: quit"))

	style := styles.AppStyle
	if m.width > 0 && m.height > 0 {
		style = style.Width(m.width).Height(m.height)
	}
	return style.Render(b.String())
}

func (m model) renderSplash(width int) string {
	barWidth := width - 12
	if barWidth < 20 {
		barWidth = 20
	}
	if barWidth > 40 {
		barWidth = 40
	}

	var b strings.Builder

	b.WriteString(banner.RenderFlying(m.splashProgress))
	b.WriteString("\n\n")
	b.WriteString(styles.TitleStyle.Render("Connecting to room"))
	b.WriteString("\n")
	b.WriteString(styles.SubtitleStyle.Render("Peer name: " + m.receiver.PeerName))
	b.WriteString("\n\n")

	bar := components.ProgressBar(barWidth, m.splashProgress)
	percent := int(m.splashProgress * 100)
	b.WriteString(styles.PeerStyle.Render(bar + fmt.Sprintf("  %3d%%", percent)))
	b.WriteString("\n\n")

	status := "Scanning the network…"
	if m.connected {
		status = "✓ Connected. Ready to receive files."
		b.WriteString(styles.SuccessStyle.Render(status))
	} else {
		b.WriteString(styles.InfoStyle.Render(status))
	}
	b.WriteString("\n\n")
	b.WriteString(styles.HelpStyle.Render("Saving files to " + m.saveDir + "  (press any key to continue)"))

	content := lipgloss.NewStyle().
		Align(lipgloss.Center).
		Width(width).
		Render(b.String())

	style := styles.AppStyle
	if m.width > 0 && m.height > 0 {
		style = style.Width(m.width).Height(m.height)
	}
	return style.Render(content)
}

func (m *model) renderTransferRow(t transfer.PeerProgress, barWidth int) string {
	filename := m.transferNames[t.TransferID]
	if filename == "" {
		filename = t.TransferID
	}
	head := styles.FilenameStyle.Render("  "+filename) + "  "

	switch t.State {
	case transfer.StateWaiting:
		return head + styles.StatusWaitingStyle.Render("⏳ waiting for offer")
	case transfer.StateAccepted:
		return head + styles.StatusAcceptedStyle.Render("✓ accepted")
	case transfer.StateInProgress:
		return head + "\n  " + components.FormatProgressWithWidth(t.Offset, t.Total, barWidth)
	case transfer.StateCompleted:
		return head + styles.StatusCompletedStyle.Render("✓ completed")
	case transfer.StateRejected:
		return head + styles.StatusFailedStyle.Render("✗ rejected")
	case transfer.StateFailed:
		return head + styles.StatusFailedStyle.Render("✗ failed")
	case transfer.StateDisconnected:
		return head + styles.StatusFailedStyle.Render("✗ disconnected")
	default:
		return head + t.State.String()
	}
}

func Run(r *receiver.Receiver, saveDir, roomKey, connectIP string) error {
	defer func() { _ = r.Close() }()

	m := NewModel(r, saveDir, roomKey, connectIP)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if err != nil {
		return err
	}

	if fm, ok := final.(model); ok && fm.connectErr != nil {
		return fmt.Errorf("failed to connect: %w", fm.connectErr)
	}
	return nil
}
