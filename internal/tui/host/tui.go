package host

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zuhailz/GoDrop/internal/banner"
	"github.com/zuhailz/GoDrop/internal/host"
	"github.com/zuhailz/GoDrop/internal/protocol"
	"github.com/zuhailz/GoDrop/internal/transfer"
	"github.com/zuhailz/GoDrop/internal/tui/components"
	"github.com/zuhailz/GoDrop/internal/tui/styles"
)

type keyMap struct {
	Input   key.Binding
	CopyID  key.Binding
	Confirm key.Binding
	Cancel  key.Binding
	Back    key.Binding
	Quit    key.Binding
	FeedUp  key.Binding
	FeedDn  key.Binding
}

var keys = keyMap{
	Input:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "command")),
	CopyID:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy room key")),
	Confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Back:    key.NewBinding(key.WithKeys("backspace"), key.WithHelp("backspace", "up a dir")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	FeedUp:  key.NewBinding(key.WithKeys("["), key.WithHelp("[", "feed older")),
	FeedDn:  key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "feed newer")),
}

type ProgressUpdate struct {
	Progress transfer.PeerProgress
}

type PeerEventMsg struct {
	Event host.PeerEvent
}

type splashTickMsg struct{}

type SystemEventMsg struct {
	Event protocol.SystemEvent
}

type model struct {
	host      *host.Host
	peers     []string
	transfers []transfer.PeerProgress
	feed      []components.FeedItem
	width     int
	height    int
	err       error

	splash         bool
	splashProgress float64

	transferNames map[string]string

	showInput bool
	input     string
	feedIdx   int

	browserMode bool
	browserDir  string
	files       []string
	browserIdx  int
}

func NewModel(h *host.Host) model {
	dir, _ := os.Getwd()
	return model{
		host:      h,
		splash:    true,
		transfers: make([]transfer.PeerProgress, 0),
		feed: []components.FeedItem{{
			Event: true,
			Text:  fmt.Sprintf("Room key: %s (press c to copy)", h.RoomKey),
			Kind:  components.FeedSuccess,
		}},
		transferNames: make(map[string]string),
		browserDir:    dir,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tickSplash(),
		waitForProgress(m.host.ProgressChan()),
		waitForPeerEvents(m.host.EventChan()),
		waitForSystemEvent(m.host.SystemEventChan()),
	)
}

func tickSplash() tea.Cmd {
	return tea.Tick(30*time.Millisecond, func(time.Time) tea.Msg {
		return splashTickMsg{}
	})
}

func waitForSystemEvent(ch <-chan protocol.SystemEvent) tea.Cmd {
	return func() tea.Msg {
		if event, ok := <-ch; ok {
			return SystemEventMsg{Event: event}
		}
		return nil
	}
}

func waitForProgress(ch <-chan transfer.PeerProgress) tea.Cmd {
	return func() tea.Msg {
		if progress, ok := <-ch; ok {
			return ProgressUpdate{Progress: progress}
		}
		return nil
	}
}

func waitForPeerEvents(ch <-chan host.PeerEvent) tea.Cmd {
	return func() tea.Msg {
		if event, ok := <-ch; ok {
			return PeerEventMsg{Event: event}
		}
		return nil
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.splash {
			m.splash = false
			return m, nil
		}
		return m.handleKeyMsg(msg)

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
		if m.splash {
			m.splash = false
		}
		found := false
		for i, t := range m.transfers {
			if t.TransferID == msg.Progress.TransferID && t.PeerID == msg.Progress.PeerID {
				m.transfers[i] = msg.Progress
				found = true
				break
			}
		}
		if !found {
			m.transfers = append(m.transfers, msg.Progress)
		}
		m.syncPeers()
		return m, waitForProgress(m.host.ProgressChan())

	case PeerEventMsg:
		if m.splash {
			m.splash = false
		}
		m.syncPeers()
		return m, waitForPeerEvents(m.host.EventChan())

	case SystemEventMsg:
		if m.splash {
			m.splash = false
		}
		m.feed = append(m.feed, components.FeedItem{Event: true, Text: msg.Event.Text, Kind: components.FeedNeutral})
		return m, waitForSystemEvent(m.host.SystemEventChan())

	case SendFileMsg:
		if msg.Path != "" {
			transferID, err := m.host.OfferFile(msg.Path)
			if err != nil {
				m.err = err
				m.feed = append(m.feed, components.FeedItem{Event: true, Text: fmt.Sprintf("Failed to offer %s: %v", filepath.Base(msg.Path), err), Kind: components.FeedError})
			} else {
				m.transferNames[transferID] = filepath.Base(msg.Path)
			}
		}

	case BrowserLoaded:
		m.browserMode = true
		m.browserDir = msg.Dir
		m.files = sortedEntries(msg.Entries)
		m.browserIdx = 0
	}

	return m, nil
}

func (m model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.browserMode {
		return m.handleBrowserKey(msg)
	}

	if m.showInput {
		return m.handleInputKey(msg)
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Input):
		m.showInput = true
		m.input = "/"
	case key.Matches(msg, keys.CopyID):
		if components.CopyToClipboard(m.host.RoomKey) {
			m.feed = append(m.feed, components.FeedItem{Event: true, Text: "Copied room key: " + m.host.RoomKey, Kind: components.FeedSuccess})
		} else {
			m.feed = append(m.feed, components.FeedItem{Event: true, Text: "Could not confirm copy — select the key: " + m.host.RoomKey, Kind: components.FeedWarning})
		}
	case key.Matches(msg, keys.FeedUp):
		if len(m.feed) > components.MaxFeedVisible && m.feedIdx < len(m.feed)-components.MaxFeedVisible {
			m.feedIdx++
		}
	case key.Matches(msg, keys.FeedDn):
		if m.feedIdx > 0 {
			m.feedIdx--
		}
	}

	return m, nil
}

func (m model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.showInput = false
		m.input = ""
		return m, nil
	case tea.KeyEnter:
		return m.submitCommand()
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil
	case tea.KeySpace:
		m.input += " "
		return m, nil
	case tea.KeyRunes:
		m.input += string(msg.Runes)
		return m, nil
	}
	return m, nil
}

func (m model) submitCommand() (tea.Model, tea.Cmd) {
	cmd := strings.TrimSpace(m.input)
	m.showInput = false
	m.input = ""

	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return m, nil
	}

	switch {
	case parts[0] == "/send" && len(parts) >= 2:
		path := strings.TrimPrefix(cmd, "/send")
		path = strings.TrimSpace(path)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return m, openBrowser(path)
		}
		transferID, err := m.host.OfferFile(path)
		if err != nil {
			m.err = err
			m.feed = append(m.feed, components.FeedItem{Event: true, Text: fmt.Sprintf("Failed to offer %s: %v", path, err), Kind: components.FeedError})
		} else {
			m.transferNames[transferID] = filepath.Base(path)
		}

	case parts[0] == "/send":
		return m, openBrowser(m.browserDir)

	case parts[0] == "/exit" || parts[0] == "/quit":
		return m, tea.Quit

	case parts[0] == "/peers":
		names := m.host.Peers()
		if len(names) == 0 {
			m.feed = append(m.feed, components.FeedItem{Event: true, Text: "No peers connected"})
		} else {
			m.feed = append(m.feed, components.FeedItem{Event: true, Text: "Connected peers: " + strings.Join(names, ", "), Kind: components.FeedSuccess})
		}

	case parts[0] == "/help":
		m.feed = append(m.feed, components.FeedItem{
			Event: true,
			Text:  "/send <path>  •  /peers  •  /exit  •  /help",
			Kind:  components.FeedSuccess,
		})

	default:
		m.feed = append(m.feed, components.FeedItem{Event: true, Text: fmt.Sprintf("Unknown command: %s (try /help)", parts[0]), Kind: components.FeedError})
	}

	return m, nil
}

func (m model) handleBrowserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.browserMode = false
		return m, nil
	case tea.KeyEnter:
		if m.browserIdx < len(m.files) {
			selected := m.files[m.browserIdx]
			path := filepath.Join(m.browserDir, selected)
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				return m, openBrowser(path)
			}
			m.browserMode = false
			return m, func() tea.Msg { return SendFileMsg{Path: path} }
		}
	case tea.KeyRunes:
		if len(msg.Runes) == 1 && msg.Runes[0] == 's' && m.browserIdx < len(m.files) {
			selected := m.files[m.browserIdx]
			path := filepath.Join(m.browserDir, selected)
			m.browserMode = false
			return m, func() tea.Msg { return SendFileMsg{Path: path} }
		}
	case tea.KeyUp:
		if m.browserIdx > 0 {
			m.browserIdx--
		}
	case tea.KeyDown:
		if m.browserIdx < len(m.files)-1 {
			m.browserIdx++
		}
	case tea.KeyBackspace:
		parent := filepath.Dir(m.browserDir)
		if parent != m.browserDir {
			return m, openBrowser(parent)
		}
	}
	return m, nil
}

type SendFileMsg struct {
	Path string
}

func openBrowser(dir string) tea.Cmd {
	return func() tea.Msg {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return SendFileMsg{Path: ""}
		}
		return BrowserLoaded{Dir: dir, Entries: entries}
	}
}

type BrowserLoaded struct {
	Dir     string
	Entries []os.DirEntry
}

func (m *model) syncPeers() {
	m.peers = m.host.Peers()
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
		return m.finish(m.renderSplash(innerWidth))
	}

	b.WriteString(styles.TitleStyle.Render("GoDrop"))
	b.WriteString("  ")
	b.WriteString(styles.SubtitleStyle.Render(fmt.Sprintf("%s  •  %s", m.host.Name, m.host.RoomKey)))
	b.WriteString("\n\n")

	if m.browserMode {
		b.WriteString(m.renderBrowser(innerWidth))
		return m.finish(b.String())
	}

	var peersBody strings.Builder
	if len(m.peers) == 0 {
		peersBody.WriteString(styles.InfoStyle.Render("  No peers yet. Share the room key to invite someone."))
	} else {
		for _, peer := range m.peers {
			peersBody.WriteString(styles.PeerStyle.Render("  • " + peer))
			peersBody.WriteString("\n")
		}
	}
	b.WriteString(components.Panel(fmt.Sprintf("CONNECTED PEERS (%d)", len(m.peers)), peersBody.String(), innerWidth))
	b.WriteString("\n\n")

	var transfersBody strings.Builder
	if len(m.transfers) == 0 {
		transfersBody.WriteString(styles.InfoStyle.Render("  Nothing in flight. /send <path> to offer a file"))
	} else {
		for i := len(m.transfers) - 1; i >= 0; i-- {
			transfersBody.WriteString(m.renderTransferRow(m.transfers[i], barWidth))
			transfersBody.WriteString("\n")
			if i > 0 {
				transfersBody.WriteString(styles.DividerStyle.Render("  ─"))
				transfersBody.WriteString("\n")
			}
		}
	}
	b.WriteString(components.Panel(fmt.Sprintf("TRANSFERS (%d)", len(m.transfers)), transfersBody.String(), innerWidth))
	b.WriteString("\n\n")

	var feedBody strings.Builder
	if len(m.feed) == 0 {
		feedBody.WriteString(styles.InfoStyle.Render("  Nothing yet. /send <path> to offer a file"))
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

	if m.showInput {
		b.WriteString(styles.TitleStyle.Render("> " + m.input + "▌"))
	} else {
		b.WriteString(styles.HelpStyle.Render("c: copy room key  •  [ or ]: scroll feed  •  / for commands (/help)  •  q: quit"))
	}

	return m.finish(b.String())
}

func (m model) finish(view string) string {
	style := styles.AppStyle
	if m.width > 0 && m.height > 0 {
		style = style.Width(m.width).Height(m.height)
	}
	return style.Render(view)
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
	b.WriteString(styles.TitleStyle.Render("Room Key: " + m.host.RoomKey))
	b.WriteString("\n")
	b.WriteString(styles.SubtitleStyle.Render("Name: " + m.host.Name))
	b.WriteString("\n\n")

	bar := components.ProgressBar(barWidth, m.splashProgress)
	percent := int(m.splashProgress * 100)
	b.WriteString(styles.PeerStyle.Render(bar + fmt.Sprintf("  %3d%%", percent)))
	b.WriteString("\n\n")
	b.WriteString(styles.InfoStyle.Render("Share this room key with receivers to start transferring files"))
	b.WriteString("\n\n")
	b.WriteString(styles.HelpStyle.Render("Starting host…  (press any key to continue)"))

	return lipgloss.NewStyle().
		Align(lipgloss.Center).
		Width(width).
		Render(b.String())
}

func (m *model) renderTransferRow(t transfer.PeerProgress, barWidth int) string {
	filename := m.transferNames[t.TransferID]
	if filename == "" {
		filename = t.TransferID
	}
	peerName := m.host.PeerName(t.PeerID)

	head := fmt.Sprintf("  %s  →  %s  ",
		styles.FilenameStyle.Render(filename),
		styles.PeerStyle.Render(peerName))

	switch t.State {
	case transfer.StateWaiting:
		return head + styles.StatusWaitingStyle.Render("⏳ waiting for accept")
	case transfer.StateAccepted:
		return head + styles.StatusAcceptedStyle.Render("✓ accepted")
	case transfer.StateInProgress:
		return head + "\n  " + components.FormatProgressWithWidth(t.Offset, t.Total, barWidth)
	case transfer.StateCompleted:
		return head + styles.StatusCompletedStyle.Render("✓ completed")
	case transfer.StateRejected:
		return head + styles.StatusFailedStyle.Render("✗ rejected")
	case transfer.StateDisconnected:
		return head + styles.StatusFailedStyle.Render("✗ disconnected")
	case transfer.StateFailed:
		return head + styles.StatusFailedStyle.Render("✗ failed")
	default:
		return head + t.State.String()
	}
}

func (m *model) renderBrowser(width int) string {
	var body strings.Builder

	if m.browserIdx >= len(m.files) {
		m.browserIdx = 0
	}

	for i, f := range m.files {
		display := f
		path := filepath.Join(m.browserDir, display)
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			display += "/"
		}
		style := styles.NormalItemStyle
		cursor := "  "
		if i == m.browserIdx {
			style = styles.SelectedItemStyle
			cursor = "▸ "
		}
		body.WriteString(style.Render(cursor + display))
		body.WriteString("\n")
	}

	body.WriteString("\n")
	body.WriteString(styles.HelpStyle.Render("  ↑/↓: navigate  •  enter: open  •  s: send file/folder  •  backspace: up  •  esc: close"))

	return components.Panel("FILE BROWSER: "+m.browserDir, body.String(), width)
}

func Run(h *host.Host) error {
	m := NewModel(h)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func sortedEntries(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}
