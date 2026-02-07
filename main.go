package main

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"flag"
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
)

var enableDebug bool

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

// --- Crypto ---

func deriveKey(password string) []byte {
	h := sha256.Sum256([]byte(password))
	return h[:]
}

func encryptData(plaintext []byte, password string) (string, error) {
	block, err := aes.NewCipher(deriveKey(password))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decryptData(encoded string, password string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(deriveKey(password))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
}

func passwordFingerprint(password string) string {
	h := sha256.Sum256([]byte("LAN-CHAT-VERIFY:" + password))
	return hex.EncodeToString(h[:])
}

// --- Messages ---
type peerUpdateMsg struct{ name, ip, lastMsg string }
type transferStatusMsg string
type chatMsg struct{ sender, content string }
type progressMsg float64
type peerVerifiedMsg struct{ ip string; secure bool }
type configToggleDebugMsg struct{}

// item implements list.Item
type item struct {
	title, desc, lastMsg string
	secure               bool
}

func (i item) Title() string {
	if i.secure {
		return "\U0001F512 " + i.title
	}
	return i.title
}
func (i item) Description() string {
	if i.secure {
		return i.desc + " | \U0001F512 Encrypted | " + i.lastMsg
	}
	return i.desc + " | " + i.lastMsg
}
func (i item) FilterValue() string { return i.title }

// --- Model ---
type model struct {
	state       int // 0: list, 1: picker, 2: progress, 3: chat, 4: config
	list        list.Model
	filepicker  filepicker.Model
	progress    progress.Model
	textInput   textinput.Model
	viewport    viewport.Model
	selectedIP   string
	selectedName string
	lastStatus   string
	chatHistory []string
	networkChan chan interface{}
	userName    string
	width       int
	height      int
	password    string
	passHash    string
	securePeers map[string]bool
	configDebug bool
}

func initialModel(name string, password string, netChan chan interface{}) model {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "xYou are: " + name + " | (/) Filter (f) File (c) Config (enter) Chat (esc) Quit"

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

	var ph string
	if password != "" {
		ph = passwordFingerprint(password)
	}

	return model{
		state:       0,
		list:        l,
		filepicker:  fp,
		progress:    progress.New(progress.WithDefaultGradient()),
		textInput:   ti,
		networkChan: netChan,
		userName:    name,
		password:    password,
		passHash:    ph,
		securePeers: make(map[string]bool),
		configDebug: enableDebug,
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
	msgType := fmt.Sprintf("%T", msg)
	if msgType != "cursor.BlinkMsg" {
		debugLog("Update: state=%d, msg=%s", m.state, msgType)
	}
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

			// 3. Otherwise, Esc acts as a "Back" button from Chat, File Picker, or Config
			m.state = 0
			m.textInput.Blur()
			m.textInput.Reset()
			return m, nil
		case "c":
			if m.state == 0 {
				m.state = 4
				return m, nil
			}
		case "f":
			if m.state == 0 && m.list.SelectedItem() != nil {
				item := m.list.SelectedItem().(item)
				m.selectedIP = item.desc
				m.selectedName = item.title
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
				item := m.list.SelectedItem().(item)
				m.selectedIP = item.desc
				m.selectedName = item.title
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

	case peerVerifiedMsg:
		debugLog("Peer verification: ip=%s secure=%v", msg.ip, msg.secure)
		m.securePeers[msg.ip] = msg.secure
		items := m.list.Items()
		for i, itm := range items {
			p := itm.(item)
			if p.desc == msg.ip {
				p.secure = msg.secure
				m.list.SetItem(i, p)
				break
			}
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

	case configToggleDebugMsg:
		m.configDebug = !m.configDebug
		enableDebug = m.configDebug
		// Ensure log output is properly redirected
		if enableDebug {
			logFile, err := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				log.SetOutput(logFile)
			}
		}
		return m, nil
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
	} else if m.state == 4 {
		// Config state - handle key inputs
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "d":
				return m, func() tea.Msg { return configToggleDebugMsg{} }
			case "up", "down":
				// Navigate through options (currently only debug)
				return m, nil
			}
		}
		return m, nil
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
	// Viewport: Remaining height = Height - 6 - 1 (footer border only) = Height - 7
	// Wait, we have footer now which is 1 line.
	// Total height = Height.
	// Used by: Title (3), Input (3), Footer (1).
	// Remaining for Viewport = Height - 7.
	// Viewport has borders (2).
	// Content height inside viewport = Height - 7 - 2 = Height - 9.
	
	// User reported it's 3 lines too short.
	// Let's re-evaluate.
	// Total Available: Height
	// Layout:
	// - Title Box (Height 3: 1 line text + 2 border lines)
	// - Viewport Box (Height X)
	// - Input Box (Height 3: 1 line text + 2 border lines)
	// - Footer (Height 1)
	
	// The View() function joins these with JoinVertical.
	// JoinVertical simply stacks strings.
	// If borders overlap (collapsing borders), height calculation is different.
	// Currently, they do NOT overlap/collapse automatically with standard styles unless handled specifically.
	// We are just returning Render() output strings.
	
	// Total Height Used = 3 (Title) + X (Viewport) + 3 (Input) + 1 (Footer) = 7 + X
	// So X (Viewport Height INCLUDING borders) = Height - 7
	
	// Viewport Content Height = X - 2 (borders) = (Height - 7) - 2 = Height - 9
	
	// If it is 3 lines too short, maybe the border calculation is wrong or margins?
	// lipgloss.JoinVertical adds newlines? No.
	
	// Let's try increasing viewport height by 3 as requested to see if it fits.
	// Previous: Height - 9. New: Height - 6.
	
	viewportHeight := height - 6
	if viewportHeight < 0 { viewportHeight = 0 }
	
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

func (m model) customBorderFooter(width int, text string) string {
	// Colors
	textColor := lipgloss.Color("240") // Light gray
	borderStyle := lipgloss.NewStyle() // Default border color
	textStyle := lipgloss.NewStyle().Foreground(textColor)

	cornerLeft := "╰"
	cornerRight := "╯"
	horiz := "─"

	// Text formatting
	displayQuery := fmt.Sprintf("[ %s ]", text)
	textLen := len(displayQuery)

	// Calculate dashes
	// Total width available for dashes = width - 2 (corners) - textLen
	// Align right-ish: Give 2 dashes padding on right if possible
	availableSpace := width - 2 - textLen
	if availableSpace < 0 {
		availableSpace = 0
	}

	rightDashes := 2
	leftDashes := availableSpace - rightDashes

	if leftDashes < 0 {
		leftDashes = 0
		rightDashes = availableSpace
	}

	line := borderStyle.Render(cornerLeft) +
		borderStyle.Render(strings.Repeat(horiz, leftDashes)) +
		textStyle.Render(displayQuery) +
		borderStyle.Render(strings.Repeat(horiz, rightDashes)) +
		borderStyle.Render(cornerRight)

	return line
}

func (m model) View() string {
	// Define border styles with minimal padding
	// Force the width to be full width minus borders (2)
	// We want all boxes to be full width
	fullWidthStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(m.width - 2)

	listStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder(), true, true, false, true).
		Padding(0, 1).
		Width(m.width - 2)
		
	borderStyle := fullWidthStyle // Used for titles
	filePickerStyle := fullWidthStyle
	progressStyle := fullWidthStyle
	chatViewportStyle := fullWidthStyle
	inputStyle := fullWidthStyle

	// Minimal margins to maximize space
	containerStyle := lipgloss.NewStyle().Margin(0, 0)

	switch m.state {
	case 1:
		title := borderStyle.Render("Select File")
		
		// Custom footer for filepicker
		footer := m.customBorderFooter(m.width, "(enter) Select | (esc) Back")
		
		// Adjust content style to remove bottom border so footer attaches correctly
		contentStyle := filePickerStyle.Copy().Border(lipgloss.RoundedBorder(), true, true, false, true)
		content := contentStyle.Render(m.filepicker.View())
		
		return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, content, footer))
	case 2:
		secureLabel := ""
		if m.password != "" && m.securePeers[m.selectedIP] {
			secureLabel = " \U0001F512 Encrypted"
		}
		title := borderStyle.Render(fmt.Sprintf("Sending to %s (%s)%s...", m.selectedName, m.selectedIP, secureLabel))
		
		// Custom footer for progress
		// No specific interactions usually, but maybe Quit?
		footer := m.customBorderFooter(m.width, "")
		
		contentStyle := progressStyle.Copy().Border(lipgloss.RoundedBorder(), true, true, false, true)
		content := contentStyle.Render(m.progress.View())
		
		return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, content, footer))
	case 3:
		chatSecure := ""
		if m.password != "" && m.securePeers[m.selectedIP] {
			chatSecure = " \U0001F512 Encrypted"
		}
		title := borderStyle.Render(fmt.Sprintf("Chat with %s (%s)%s", m.selectedName, m.selectedIP, chatSecure))
		
		// Custom footer for chat
		footer := m.customBorderFooter(m.width, "(esc) Back")
		
		// Adjust viewport and input borders.
		// Viewport needs top, left, right. Input needs left, right. Footer has bottom.
		// Wait, viewport is on top of input.
		// Structure: Title (top border) -> Viewport (side borders) -> Input (side borders) -> Footer (bottom border)
		
		// Title already has full border. We should probably remove bottom border from Title?
		// No, standard Bubble Tea list usually keeps title separated.
		// Let's stick to the pattern: Title Box + Content Box + Footer.
		// But Chat has two components (Viewport + Input).
		// Let's wrap them in a container that has side borders?
		
		// Current design:
		// Title (Border)
		// Viewport (Border)
		// Input (Border)
		
		// New design requested:
		// Title (Border)
		// Viewport + Input (merged or separate?)
		// Footer (Border with text)
		
		// If we follow the list pattern:
		// Top: Title
		// Middle: Content (Viewport + Input)
		// Bottom: Footer
		
		// Let's try to make Input look like the bottom part of the content.
		
		vpStyle := chatViewportStyle.Copy().Border(lipgloss.RoundedBorder(), true, true, false, true)
		inputStyle := inputStyle.Copy().Border(lipgloss.RoundedBorder(), false, true, false, true)
		
		viewport := vpStyle.Render(m.viewport.View())
		input := inputStyle.Render(m.textInput.View())
		
		return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, viewport, input, footer))
	case 4:
		title := borderStyle.Render("Configuration")
		
		// Config options
		debugStatus := "OFF"
		debugColor := lipgloss.Color("245") // Gray for OFF
		if m.configDebug {
			debugStatus = "ON"
			debugColor = lipgloss.Color("10") // Green for ON
		}
		
		debugStyle := lipgloss.NewStyle().Foreground(debugColor)
		debugText := fmt.Sprintf("Debug Logging: %s", debugStyle.Render(debugStatus))
		
		// Create content area
		contentStyle := fullWidthStyle.Copy().Border(lipgloss.RoundedBorder(), true, true, false, true)
		content := contentStyle.Render(
			lipgloss.JoinVertical(lipgloss.Left,
				"",
				debugText,
				"",
				"Press (d) to toggle debug logging",
				"Press (esc) to go back",
				"",
			),
		)
		
		footer := m.customBorderFooter(m.width, "(d) Toggle Debug | (esc) Back")
		
		return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, content, footer))
	default:
		// Custom rendering for list to support "connected peers" text
		var titleText string
		var footerText string
		
		if m.list.FilterState() == list.Filtering {
			titleText = "Filter"
			footerText = "(enter) Apply | (esc) Cancel"
		} else {
			if m.password != "" {
				titleText = fmt.Sprintf("You are: %s (Encrypted) \U0001F512", m.userName)
			} else {
				titleText = fmt.Sprintf("You are: %s", m.userName)
			}
			footerText = "(/) Filter | (f) File | (c) Config | (enter) Chat | (esc) Quit"
		}
		
		title := borderStyle.Render(titleText)
		listView := m.list.View()
		
		// Wrap list in style to match other components
		content := listStyle.Render(listView)
		
		// Render custom footer
		footer := m.customBorderFooter(m.width, footerText)
		
		// Join all parts
		return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, content, footer))
	}
}

// --- Networking ---

func verifyPeer(peerIP string, passHash string, netChan chan interface{}) {
	debugLog("Verifying peer %s...", peerIP)
	conn, err := net.DialTimeout("tcp", peerIP+":"+portTCP, 2*time.Second)
	if err != nil {
		debugLog("Verify failed for %s: %v", peerIP, err)
		netChan <- peerVerifiedMsg{ip: peerIP, secure: false}
		return
	}
	defer conn.Close()
	fmt.Fprintf(conn, "VERIFY:%s\n", passHash)
	resp, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		debugLog("Verify read error for %s: %v", peerIP, err)
		netChan <- peerVerifiedMsg{ip: peerIP, secure: false}
		return
	}
	match := strings.TrimSpace(resp) == "VMATCH"
	debugLog("Verify result for %s: match=%v", peerIP, match)
	netChan <- peerVerifiedMsg{ip: peerIP, secure: match}
}

func (m model) sendChatCmd(text string) tea.Cmd {
	return func() tea.Msg {
		conn, err := net.DialTimeout("tcp", m.selectedIP+":"+portTCP, 2*time.Second)
		if err != nil {
			return transferStatusMsg("Chat error: " + err.Error())
		}
		defer conn.Close()
		if m.password != "" && m.securePeers[m.selectedIP] {
			debugLog("Sending encrypted chat to %s", m.selectedIP)
			encrypted, err := encryptData([]byte(text), m.password)
			if err != nil {
				debugLog("Chat encryption error: %v", err)
				return transferStatusMsg("Encryption error: " + err.Error())
			}
			fmt.Fprintf(conn, "ECHAT:%s:%s\n", m.userName, encrypted)
		} else {
			debugLog("Sending plaintext chat to %s", m.selectedIP)
			fmt.Fprintf(conn, "CHAT:%s:%s\n", m.userName, text)
		}
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
		if m.password != "" && m.securePeers[m.selectedIP] {
			debugLog("Sending encrypted file %s to %s", fInfo.Name(), m.selectedIP)
			fmt.Fprintf(conn, "EFILE:%s\n", fInfo.Name())
			bufio.NewReader(conn).ReadString('\n') // wait for ACCEPTED
			// Load file into memory for encryption (acceptable for LAN-sized files)
			content, _ := io.ReadAll(file)
			encrypted, _ := encryptData(content, m.password)
			conn.Write([]byte(encrypted))
		} else {
			debugLog("Sending plaintext file %s to %s", fInfo.Name(), m.selectedIP)
			fmt.Fprintf(conn, "FILE:%s\n", fInfo.Name())
			bufio.NewReader(conn).ReadString('\n')
			io.Copy(conn, file)
		}
		return transferStatusMsg("Sent: " + fInfo.Name())
	}
}

func startTCPServer(netChan chan interface{}, password string, passHash string) {
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
				f.Close()
				netChan <- transferStatusMsg("Received: " + name)
			} else if strings.HasPrefix(header, "EFILE:") {
				fmt.Fprintln(c, "ACCEPTED")
				name := strings.TrimSpace(strings.TrimPrefix(header, "EFILE:"))
				debugLog("Receiving encrypted file: %s", name)
				encoded, _ := io.ReadAll(reader)
				if password != "" {
					plaintext, err := decryptData(string(encoded), password)
					if err != nil {
						debugLog("File decryption failed for %s: %v", name, err)
						netChan <- transferStatusMsg("Failed to decrypt file: " + name)
					} else {
						debugLog("File decrypted successfully: %s", name)
						f, _ := os.Create("received_" + name)
						f.Write(plaintext)
						f.Close()
						netChan <- transferStatusMsg("Received (encrypted): " + name)
					}
				} else {
					debugLog("Encrypted file received but no password set: %s", name)
					netChan <- transferStatusMsg("Encrypted file received but no password set: " + name)
				}
			} else if strings.HasPrefix(header, "CHAT:") {
				parts := strings.SplitN(header[5:], ":", 2)
				if len(parts) == 2 {
					netChan <- chatMsg{sender: parts[0], content: strings.TrimSpace(parts[1])}
				}
			} else if strings.HasPrefix(header, "ECHAT:") {
				parts := strings.SplitN(header[6:], ":", 2)
				if len(parts) == 2 {
					sender := parts[0]
					payload := strings.TrimSpace(parts[1])
					debugLog("Received encrypted chat from %s", sender)
					if password != "" {
						plaintext, err := decryptData(payload, password)
						if err != nil {
							debugLog("Chat decryption failed from %s: %v", sender, err)
							netChan <- chatMsg{sender: sender, content: "[Could not decrypt - password mismatch]"}
						} else {
							debugLog("Chat decrypted successfully from %s", sender)
							netChan <- chatMsg{sender: sender, content: string(plaintext)}
						}
					} else {
						debugLog("Encrypted chat from %s but no password set", sender)
						netChan <- chatMsg{sender: sender, content: "[Encrypted message - no password set]"}
					}
				}
			} else if strings.HasPrefix(header, "VERIFY:") {
				remoteHash := strings.TrimSpace(strings.TrimPrefix(header, "VERIFY:"))
				if passHash != "" && subtle.ConstantTimeCompare([]byte(remoteHash), []byte(passHash)) == 1 {
					debugLog("VERIFY from %s: passwords match", c.RemoteAddr())
					fmt.Fprintln(c, "VMATCH")
				} else {
					debugLog("VERIFY from %s: passwords do not match", c.RemoteAddr())
					fmt.Fprintln(c, "VNOMATCH")
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

func listenUDP(myName string, passHash string, netChan chan interface{}) {
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
				debugLog("Discovered peer: %s (%s)", pName, rAddr.IP.String())
				netChan <- peerUpdateMsg{name: pName, ip: rAddr.IP.String(), lastMsg: "Connected"}
				if passHash != "" {
					go verifyPeer(rAddr.IP.String(), passHash, netChan)
				} else {
					debugLog("No password set, skipping verification for %s", pName)
				}
			}
		}
	}
}

func main() {
	password := flag.String("pass", "", "Shared password for encrypted communication")
	flag.BoolVar(&enableDebug, "debug", false, "Enable debug logging to debug.log")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: lan-chat [--pass=PASSWORD] [--debug] <yourname>")
		flag.PrintDefaults()
		return
	}
	name := args[0]
	pass := *password

	var passHash string
	if pass != "" {
		passHash = passwordFingerprint(pass)
	}

	if enableDebug {
		logFile, err := os.OpenFile("debug.log", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			log.SetOutput(logFile)
			debugLog("Starting LAN-CHAT for user: %s", name)
			if pass != "" {
				debugLog("Encryption ENABLED (--pass set)")
			} else {
				debugLog("Encryption DISABLED (no --pass flag)")
			}
		}
	}

	netChan := make(chan interface{})
	go broadcast(name)
	go listenUDP(name, passHash, netChan)
	go startTCPServer(netChan, pass, passHash)

	programOpts := []tea.ProgramOption{tea.WithAltScreen()}

	p := tea.NewProgram(initialModel(name, pass, netChan), programOpts...)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
	}
}
