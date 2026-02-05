package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Constants & Messages ---
const (
	portUDP = "9999"
	portTCP = "8080"
)

type peerUpdateMsg struct{ name, ip string }
type transferStatusMsg string
type progressMsg float64

type item struct{ title, desc string }
func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

// --- The Model ---
type model struct {
	state       int // 0: list, 1: picker, 2: progress
	list        list.Model
	filepicker  filepicker.Model
	progress    progress.Model
	selectedIP  string
	lastStatus  string
	networkChan chan interface{}
	userName    string
}

func initialModel(name string, netChan chan interface{}) model {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Peer: " + name + " | (f) Send File (q) Quit"

	fp := filepicker.New()
	fp.CurrentDirectory, _ = os.Getwd()

	return model{
		state:       0,
		list:        l,
		filepicker:  fp,
		progress:    progress.New(progress.WithDefaultGradient()),
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

// --- Update & View ---
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q": return m, tea.Quit
		case "f":
			if m.state == 0 && m.list.SelectedItem() != nil {
				m.selectedIP = m.list.SelectedItem().(item).desc
				m.state = 1
			}
		case "esc": m.state = 0
		}

	case peerUpdateMsg:
		m.list.InsertItem(0, item{title: msg.name, desc: msg.ip})
		return m, waitForNetwork(m.networkChan)

	case progressMsg:
		cmd := m.progress.SetPercent(float64(msg))
		return m, cmd

	case transferStatusMsg:
		m.state = 0
		m.lastStatus = string(msg)
		return m, nil

	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width-4, msg.Height-8)
		m.progress.Width = msg.Width - 10
	}

	var cmd tea.Cmd
	if m.state == 1 {
		m.filepicker, cmd = m.filepicker.Update(msg)
		if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
			m.state = 2
			return m, m.sendFileCmd(path)
		}
		return m, cmd
	}

	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	s := lipgloss.NewStyle().Margin(1, 2)
	switch m.state {
	case 1: return s.Render("Select File:\n\n" + m.filepicker.View())
	case 2: return s.Render(fmt.Sprintf("Sending to %s...\n\n%s", m.selectedIP, m.progress.View()))
	default:
		return s.Render(m.list.View() + "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Render(m.lastStatus))
	}
}

// --- Networking Logic ---

func (m model) sendFileCmd(path string) tea.Cmd {
	return func() tea.Msg {
		file, _ := os.Open(path)
		defer file.Close()
		fInfo, _ := file.Stat()

		conn, err := net.DialTimeout("tcp", m.selectedIP+":"+portTCP, 3*time.Second)
		if err != nil { return transferStatusMsg("Dial Error: " + err.Error()) }
		defer conn.Close()

		fmt.Fprintf(conn, "FILE:%s\n", fInfo.Name())
		// Wait for ACCEPTED
		resp, _ := bufio.NewReader(conn).ReadString('\n')
		if !strings.Contains(resp, "ACCEPTED") { return transferStatusMsg("Rejected by peer") }

		io.Copy(conn, file)
		return transferStatusMsg("Sent: " + fInfo.Name())
	}
}

func startTCPServer(netChan chan interface{}) {
	ln, _ := net.Listen("tcp", ":"+portTCP)
	for {
		conn, _ := ln.Accept()
		go func(c net.Conn) {
			defer c.Close()
			reader := bufio.NewReader(c)
			header, _ := reader.ReadString('\n')

			if strings.HasPrefix(header, "FILE:") {
				fmt.Fprintln(c, "ACCEPTED")
				fileName := strings.TrimSpace(strings.TrimPrefix(header, "FILE:"))
				f, _ := os.Create("received_" + fileName)
				defer f.Close()
				io.Copy(f, reader)
				netChan <- transferStatusMsg("Received: " + fileName)
			}
		}(conn)
	}
}

func broadcast(name string) {
	addr, _ := net.ResolveUDPAddr("udp", "255.255.255.255:"+portUDP)
	conn, _ := net.DialUDP("udp", nil, addr)
	for {
		conn.Write([]byte("IAM:" + name))
		time.Sleep(3 * time.Second)
	}
}

func listenUDP(name string, netChan chan interface{}) {
	addr, _ := net.ResolveUDPAddr("udp", ":"+portUDP)
	conn, _ := net.ListenUDP("udp", addr)
	buf := make([]byte, 1024)
	for {
		n, rAddr, _ := conn.ReadFromUDP(buf)
		msg := string(buf[:n])
		if strings.HasPrefix(msg, "IAM:") && !strings.Contains(msg, name) {
			netChan <- peerUpdateMsg{name: msg[4:], ip: rAddr.IP.String()}
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <yourname>")
		return
	}
	myName := os.Args[1]
	netChan := make(chan interface{})

	go broadcast(myName)
	go listenUDP(myName, netChan)
	go startTCPServer(netChan)

	p := tea.NewProgram(initialModel(myName, netChan), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}