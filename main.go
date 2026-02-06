package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	portUDP     = "9999"
	portTCP     = "8080"
	enableDebug = true // Set to false to disable debugging
)

// --- Debugging ---
func debugLog(format string, v ...interface{}) {
	if enableDebug {
		log.Printf("[DEBUG] "+format, v...)
	}
}

func logToFile(s string) {
	if enableDebug {
		f, _ := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		defer f.Close()
		f.WriteString(s + "\n")
	}
}

// --- Messages ---
type peerUpdateMsg struct{ name, ip, lastMsg string }
type transferStatusMsg string
type chatMsg struct{ sender, content string }
type progressMsg float64

// item implements list.Item
type item struct {
	title, desc, lastMsg string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc + " | " + i.lastMsg }
func (i item) FilterValue() string { return i.title }

// --- Model ---
type model struct {
	state       int // 0: list, 1: picker, 2: progress, 3: chat
	list        list.Model
	filepicker  filepicker.Model
	progress    progress.Model
	textInput   textinput.Model
	viewport    viewport.Model
	selectedIP  string
	lastStatus  string
	chatHistory []string
	networkChan chan interface{}
	userName    string
	width       int
	height      int
}

func initialModel(name string, netChan chan interface{}) model {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "xYou are: " + name + " | (/) Filter (f) File (enter) Chat (esc) Quit"

	// Remove 'q' from the help menu
	l.KeyMap.Quit.SetKeys()
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)  // Hide default help view since we render it manually
	l.SetShowTitle(false) // Hide default title since we render it manually

	fp := filepicker.New()
	fp.CurrentDirectory, _ = os.Getwd()

	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	// Don't focus by default, only focus when in chat mode

	return model{
		state:       0,
		list:        l,
		filepicker:  fp,
		progress:    progress.New(progress.WithDefaultGradient()),
		textInput:   ti,
		networkChan: netChan,
		userName:    name,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.filepicker.Init(), waitForNetwork(m.networkChan))
}

func waitForNetwork(ch chan interface{}) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// --- Update ---
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	debugLog("Update: state=%d, msg=%T", m.state, msg)
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			// 1. If the list is currently in "Filtering" mode, let the list handle it
			if m.state == 0 && m.list.FilterState() == list.Filtering {
				// We don't return tea.Quit here; we just let the message fall through
				// to m.list.Update(msg) at the bottom of the function.
				break
			}

			// 2. If we are in the main list and NOT filtering, Esc exits the whole app
			if m.state == 0 {
				return m, tea.Quit
			}

			// 3. Otherwise, Esc acts as a "Back" button from Chat or File Picker
			m.state = 0
			m.textInput.Blur()
			m.textInput.Reset()
			return m, nil
		case "f":
			if m.state == 0 && m.list.SelectedItem() != nil {
				m.selectedIP = m.list.SelectedItem().(item).desc
				m.state = 1
				return m, m.filepicker.Init()
			}
		case "enter":
			// If filtering, let the list handle Enter to stop filtering.
			// Do NOT switch to chat mode in this case.
			if m.state == 0 && m.list.FilterState() == list.Filtering {
				break
			}

			if m.state == 0 && m.list.SelectedItem() != nil {
				m.selectedIP = m.list.SelectedItem().(item).desc
				m.state = 3
				m.textInput.Focus() // Focus input when entering chat mode
				return m, nil
			} else if m.state == 3 && m.textInput.Value() != "" {
				text := m.textInput.Value()
				m.textInput.Reset()
				m.chatHistory = append(m.chatHistory, "Me: "+text)
				m.viewport.SetContent(strings.Join(m.chatHistory, "\n"))
				m.viewport.GotoBottom()
				return m, m.sendChatCmd(text)
			}
		}

	case peerUpdateMsg:
		// Check if peer exists to update last message
		items := m.list.Items()
		found := false
		for i, itm := range items {
			p := itm.(item)
			if p.desc == msg.ip {
				p.lastMsg = msg.lastMsg
				m.list.SetItem(i, p)
				found = true
				break
			}
		}
		if !found {
			m.list.InsertItem(0, item{title: msg.name, desc: msg.ip, lastMsg: "New connection"})
		}
		return m, waitForNetwork(m.networkChan)

	case chatMsg:
		m.chatHistory = append(m.chatHistory, msg.sender+": "+msg.content)
		m.viewport.SetContent(strings.Join(m.chatHistory, "\n"))
		m.viewport.GotoBottom()
		// Also update the preview in the list - find existing peer by name
		items := m.list.Items()
		for _, itm := range items {
			if p := itm.(item); p.title == msg.sender {
				return m, func() tea.Msg { return peerUpdateMsg{name: msg.sender, ip: p.desc, lastMsg: msg.content} }
			}
		}
		return m, nil

	case transferStatusMsg:
		m.state = 0
		m.lastStatus = string(msg)
		return m, waitForNetwork(m.networkChan)

	case tea.WindowSizeMsg:
		debugLog("WindowSize: %dx%d", msg.Width, msg.Height)
		m.width = msg.Width
		m.height = msg.Height
		m.resizeComponents(msg.Width, msg.Height)
	}

	if m.state == 1 {
		m.filepicker, cmd = m.filepicker.Update(msg)
		if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
			m.state = 2
			return m, m.sendFileCmd(path)
		}
		return m, cmd
	} else if m.state == 3 {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *model) resizeComponents(width, height int) {
	// Common width accounting for borders (2) and padding (2)
	// We want the outer frame to be full width.
	// The content width inside a bordered style with padding(0,1) is width - 2 (border) - 2 (padding) = width - 4.
	contentWidth := width - 4

	// List View
	m.list.SetSize(contentWidth, height-5) // -2 borders (wrapper) -3 custom title

	// File Picker View
	// Title takes ~3 lines (including borders). Height of content area = Height - 3 (title) - 2 (content border) = Height - 5.
	// Wait, title has borders (2) + text (1) = 3 lines.
	// Content has borders (2) + padding(0) + text.
	// Total height = Height.
	// Available height for filepicker content = Height - 3 (title) - 2 (content border).
	fpHeight := height - 6 // Reduced by 1 to prevent overflow
	if fpHeight < 0 {
		fpHeight = 0
	}
	m.filepicker.Height = fpHeight

	// Progress View
	m.progress.Width = contentWidth

	// Chat View
	// Title: 3 lines (1 text + 2 border)
	// Input: 3 lines (1 text + 2 border)
	// Viewport: Remaining height = Height - 6 - 2 (viewport border) = Height - 8

	viewportHeight := height - 8
	if viewportHeight < 0 {
		viewportHeight = 0
	}

	// Recreate viewport if size changed or init
	m.viewport = viewport.New(contentWidth, viewportHeight)
	m.viewport.SetContent(strings.Join(m.chatHistory, "\n"))
	m.viewport.GotoBottom()

	// Input width
	// TextInput width is the number of characters.
	// We have a border around it. Padding is (0,1).
	// So visible width is contentWidth.
	m.textInput.Width = contentWidth
}

func (m model) View() string {
	// Define border styles with minimal padding
	// Force the width to be full width minus borders (2)
	// We want all boxes to be full width
	fullWidthStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(m.width - 2)

	listStyle := fullWidthStyle
	borderStyle := fullWidthStyle // Used for titles
	filePickerStyle := fullWidthStyle
	progressStyle := fullWidthStyle
	chatViewportStyle := fullWidthStyle
	inputStyle := fullWidthStyle

	// Minimal margins to maximize space
	containerStyle := lipgloss.NewStyle().Margin(0, 0)

	switch m.state {
	case 1:
		title := borderStyle.Render("Select File (Enter to select, Esc to go back)")
		// Filepicker content needs to be rendered inside the style
		// Wait, if we wrap filepicker in a style, does it respect the width?
		// Filepicker View returns a string.
		content := filePickerStyle.Render(m.filepicker.View())
		return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, content))
	case 2:
		title := borderStyle.Render(fmt.Sprintf("Sending to %s...", m.selectedIP))
		content := progressStyle.Render(m.progress.View())
		return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, content))
	case 3:
		title := borderStyle.Render(fmt.Sprintf("Chat with %s (Esc to go back)", m.selectedIP))
		// Viewport is already sized in Update/resizeComponents
		viewport := chatViewportStyle.Render(m.viewport.View())
		input := inputStyle.Render(m.textInput.View())
		// Join with minimal spacing
		return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, viewport, input))
	default:
		// Custom rendering for list to support "connected peers" text
		var titleText string
		if m.list.FilterState() == list.Filtering {
			titleText = "Filter: Press (enter) to apply, (esc) to cancel"
		} else {
			titleText = fmt.Sprintf("You are: %s | (/) Filter (f) File (enter) Chat (esc) Quit", m.userName)
		}

		title := borderStyle.Render(titleText)
		listView := m.list.View()

		// Wrap list in style to match other components
		content := listStyle.Render(listView)

		return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, content))
	}
}

// --- Networking ---

func (m model) sendChatCmd(text string) tea.Cmd {
	return func() tea.Msg {
		conn, err := net.DialTimeout("tcp", m.selectedIP+":"+portTCP, 2*time.Second)
		if err != nil {
			return transferStatusMsg("Chat error: " + err.Error())
		}
		defer conn.Close()
		fmt.Fprintf(conn, "CHAT:%s:%s\n", m.userName, text)
		return nil
	}
}

func (m model) sendFileCmd(path string) tea.Cmd {
	return func() tea.Msg {
		file, _ := os.Open(path)
		defer file.Close()
		fInfo, _ := file.Stat()
		conn, _ := net.Dial("tcp", m.selectedIP+":"+portTCP)
		defer conn.Close()
		fmt.Fprintf(conn, "FILE:%s\n", fInfo.Name())
		bufio.NewReader(conn).ReadString('\n')
		io.Copy(conn, file)
		return transferStatusMsg("Sent: " + fInfo.Name())
	}
}

func startTCPServer(netChan chan interface{}) {
	ln, err := net.Listen("tcp", ":"+portTCP)
	if err != nil {
		netChan <- transferStatusMsg("TCP listen error: " + err.Error())
		return
	}
	for {
		conn, _ := ln.Accept()
		go func(c net.Conn) {
			defer c.Close()
			reader := bufio.NewReader(c)
			header, _ := reader.ReadString('\n')
			if strings.HasPrefix(header, "FILE:") {
				fmt.Fprintln(c, "ACCEPTED")
				name := strings.TrimSpace(strings.TrimPrefix(header, "FILE:"))
				f, _ := os.Create("received_" + name)
				io.Copy(f, reader)
				netChan <- transferStatusMsg("Received: " + name)
			} else if strings.HasPrefix(header, "CHAT:") {
				parts := strings.SplitN(header[5:], ":", 2)
				if len(parts) == 2 {
					netChan <- chatMsg{sender: parts[0], content: strings.TrimSpace(parts[1])}
				}
			}
		}(conn)
	}
}

func broadcast(name string) {
	addr, _ := net.ResolveUDPAddr("udp", "255.255.255.255:"+portUDP)
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return
	}
	for {
		conn.Write([]byte("IAM:" + name))
		time.Sleep(3 * time.Second)
	}
}

func listenUDP(myName string, netChan chan interface{}) {
	addr, _ := net.ResolveUDPAddr("udp", ":"+portUDP)
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		netChan <- transferStatusMsg("UDP listen error: " + err.Error())
		return
	}
	buf := make([]byte, 1024)
	var discovered sync.Map
	for {
		n, rAddr, _ := conn.ReadFromUDP(buf)
		msg := string(buf[:n])
		if strings.HasPrefix(msg, "IAM:") {
			pName := msg[4:]
			if pName == myName {
				continue
			}
			if _, seen := discovered.LoadOrStore(rAddr.IP.String(), pName); !seen {
				netChan <- peerUpdateMsg{name: pName, ip: rAddr.IP.String(), lastMsg: "Connected"}
			}
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <yourname>")
		return
	}
	name := os.Args[1]

	if enableDebug {
		logFile, err := os.OpenFile("debug.log", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			log.SetOutput(logFile)
			debugLog("Starting LAN-CHAT for user: %s", name)
		}
	}

	netChan := make(chan interface{})
	go broadcast(name)
	go listenUDP(name, netChan)
	go startTCPServer(netChan)

	programOpts := []tea.ProgramOption{tea.WithAltScreen()}

	p := tea.NewProgram(initialModel(name, netChan), programOpts...)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
	}
}
