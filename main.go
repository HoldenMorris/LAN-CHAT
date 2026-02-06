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
	portUDP = "9999"
	portTCP = "8080"
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
}

func initialModel(name string, netChan chan interface{}) model {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "You are: " + name + " | (f) File (enter) Chat (esc) Back/Quit"

	// Remove 'q' from the help menu
	l.KeyMap.Quit.SetKeys()

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
		// Maximize component sizes accounting for borders and minimal margins
		listWidth := msg.Width - 4  // 2 for margins, 2 for borders
		listHeight := msg.Height - 6  // 2 for margins, 2 for borders, 2 for status
		m.list.SetSize(listWidth, listHeight)
		m.filepicker.Height = msg.Height - 8
		m.progress.Width = msg.Width - 8
		// For chat: split height between viewport and input
		viewportHeight := msg.Height - 10  // Leave space for title, input, margins
		m.viewport = viewport.New(msg.Width-4, viewportHeight)
		debugLog("Component sizes: list=%dx%d, viewport=%dx%d", 
			listWidth, listHeight, msg.Width-4, viewportHeight)
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

func (m model) View() string {
	debugLog("View: state=%d, listItems=%d, viewportSize=%dx%d", 
		m.state, len(m.list.Items()), m.viewport.Width, m.viewport.Height)
	
	// Define border styles with minimal padding
	borderStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	filePickerStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	progressStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	chatViewportStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	inputStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	listStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Border(lipgloss.RoundedBorder()).Padding(0, 1)

	// Minimal margins to maximize space
	containerStyle := lipgloss.NewStyle().Margin(0, 1)

	switch m.state {
	case 1:
		title := borderStyle.Render("Select File (Enter to select, Esc to go back)")
		content := filePickerStyle.Render(m.filepicker.View())
		return containerStyle.Render(title + "\n" + content)
	case 2:
		title := borderStyle.Render(fmt.Sprintf("Sending to %s...", m.selectedIP))
		content := progressStyle.Render(m.progress.View())
		return containerStyle.Render(title + "\n" + content)
	case 3:
		title := borderStyle.Render(fmt.Sprintf("Chat with %s (Esc to go back)", m.selectedIP))
		viewport := chatViewportStyle.Render(m.viewport.View())
		input := inputStyle.Render(m.textInput.View())
		// Join with minimal spacing
		return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, viewport, input))
	default:
		list := listStyle.Render(m.list.View())
		status := statusStyle.Render(m.lastStatus)
		return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, list, status))
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
