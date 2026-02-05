package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	portUDP = "9999"
	portTCP = "8080"
)

var (
	peerRegistry    = make(map[string]string)
	discoveredPeers = make(map[string]time.Time)
	peerMutex       sync.Mutex

	// System to handle incoming requests without crashing Stdin
	pendingConn net.Conn
	pendingName string
	connMutex   sync.Mutex
)

func main() {
	var name string
	flag.StringVar(&name, "name", "", "A required name string argument")
	flag.Parse()

	if name == "" {
		fmt.Fprintln(os.Stderr, "Error: --name=ABC is required")
		os.Exit(1)
	}

	fmt.Printf("--- Peer: %s ---\n", name)
	fmt.Println("Commands: 'accept', 'reject', 'file <name> <path>', or '<name>' to chat")

	go listenForPeers(name)
	go broadcastIdentity(name)
	go startTCPServer()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		parts := strings.Split(input, " ")

		switch parts[0] {
		case "exit":
			os.Exit(0)
		case "accept":
			handleAccept()
		case "reject":
			handleReject()
		case "file":
			if len(parts) == 3 {
				target := resolveTarget(parts[1])
				sendFile(target, parts[2])
			} else {
				fmt.Println("Usage: file <name/ip> <path>")
			}
		default:
			target := resolveTarget(parts[0])
			startChatClient(target)
		}
	}
}

func resolveTarget(input string) string {
	peerMutex.Lock()
	defer peerMutex.Unlock()
	if ip, exists := peerRegistry[input]; exists {
		return ip
	}
	return input
}

// --- CONNECTION HANDLING ---

func handleAccept() {
	connMutex.Lock()
	defer connMutex.Unlock()

	if pendingConn == nil {
		fmt.Println("No pending requests.")
		return
	}

	fmt.Fprintln(pendingConn, "ACCEPTED")
	fmt.Printf("--- Chat with %s (Type '.exit' to stop) ---\n", pendingName)

	runChatSession(pendingConn)
	pendingConn = nil
}

func handleReject() {
	connMutex.Lock()
	defer connMutex.Unlock()

	if pendingConn == nil {
		fmt.Println("No request to reject.")
		return
	}
	fmt.Fprintln(pendingConn, "REJECTED")
	pendingConn.Close()
	pendingConn = nil
	fmt.Println("Request rejected.")
}

// --- DISCOVERY ---

func broadcastIdentity(hostname string) {
	addr, _ := net.ResolveUDPAddr("udp", "255.255.255.255:"+portUDP)
	conn, _ := net.DialUDP("udp", nil, addr)
	for {
		conn.Write([]byte("IAM:" + hostname))
		time.Sleep(3 * time.Second)
	}
}

func listenForPeers(myHost string) {
	addr, _ := net.ResolveUDPAddr("udp", ":"+portUDP)
	conn, _ := net.ListenUDP("udp", addr)
	readBuf := make([]byte, 1024)
	for {
		n, remoteAddr, _ := conn.ReadFromUDP(readBuf)
		msg := string(readBuf[:n])
		if strings.HasPrefix(msg, "IAM:") {
			peerName := msg[4:]
			ip := remoteAddr.IP.String()

			peerMutex.Lock()
			lastSeen, exists := discoveredPeers[ip]
			peerRegistry[peerName] = ip
			if peerName != myHost && (!exists || time.Since(lastSeen) > 60*time.Second) {
				fmt.Printf("\n[Peer Online] %s (%s)", peerName, ip)
				fmt.Print("\n> ")
			}
			discoveredPeers[ip] = time.Now()
			peerMutex.Unlock()
		}
	}
}

// --- TCP SERVER ---

func startTCPServer() {
	ln, _ := net.Listen("tcp", ":"+portTCP)
	for {
		conn, _ := ln.Accept()
		go func(c net.Conn) {
			reader := bufio.NewReader(c)
			header, _ := reader.ReadString('\n')
			header = strings.TrimSpace(header)

			if strings.HasPrefix(header, "FILE:") {
				fmt.Fprintln(c, "ACCEPTED")
				fileName := strings.TrimPrefix(header, "FILE:")
				f, _ := os.Create("received_" + fileName)
				io.Copy(f, reader)
				f.Close()
				fmt.Printf("\n[System] Received file: received_%s\n> ", fileName)
				c.Close()
			} else {
				connMutex.Lock()
				pendingConn = c
				pendingName = c.RemoteAddr().String()
				connMutex.Unlock()
				fmt.Printf("\n[Request] Chat from %s. Type 'accept' or 'reject'.\n> ", pendingName)
			}
		}(conn)
	}
}

// --- CHAT LOGIC ---

func runChatSession(conn net.Conn) {
	// Read from peer in background
	go func() {
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			fmt.Printf("\nPeer: %s\n> ", scanner.Text())
		}
		fmt.Println("\n[Peer disconnected or chat ended]")
	}()

	// Write to peer from main thread
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		text := scanner.Text()
		if text == ".exit" {
			break
		}
		fmt.Fprintln(conn, text)
		fmt.Print("> ")
	}
}

func startChatClient(target string) {
	conn, err := net.DialTimeout("tcp", target+":"+portTCP, 3*time.Second)
	if err != nil {
		fmt.Println("Could not connect:", err)
		return
	}
	fmt.Fprintf(conn, "CHAT_REQUEST\n")

	reader := bufio.NewReader(conn)
	status, _ := reader.ReadString('\n')
	if !strings.Contains(status, "ACCEPTED") {
		fmt.Println("Request denied.")
		conn.Close()
		return
	}

	fmt.Println("--- Connected! Type '.exit' to leave ---")
	runChatSession(conn)
	conn.Close()
}

func sendFile(target string, path string) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Println("File error:", err)
		return
	}
	defer file.Close()

	conn, err := net.DialTimeout("tcp", target+":"+portTCP, 3*time.Second)
	if err != nil {
		fmt.Println("Connection error:", err)
		return
	}
	defer conn.Close()

	fInfo, _ := file.Stat()
	fmt.Fprintf(conn, "FILE:%s\n", fInfo.Name())

	reader := bufio.NewReader(conn)
	status, _ := reader.ReadString('\n')
	if strings.Contains(status, "ACCEPTED") {
		io.Copy(conn, file)
		fmt.Println("File sent successfully.")
	} else {
		fmt.Println("Peer rejected the file.")
	}
}