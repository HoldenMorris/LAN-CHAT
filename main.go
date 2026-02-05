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
	// Maps Name -> IP address
	peerRegistry    = make(map[string]string)
	discoveredPeers = make(map[string]time.Time)
	peerMutex       sync.Mutex
)

func main() {
	var name string
	flag.StringVar(&name, "name", "", "A required name string argument")
	flag.Parse()

	if name == "" {
		fmt.Fprintln(os.Stderr, "Error: --name=ABC is required")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("--- Peer: %s ---\n", name)
	fmt.Println("Commands: 'exit', 'file <name/ip> <path>', or just '<name/ip>' to chat")

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

		if parts[0] == "exit" {
			os.Exit(0)
		} else if parts[0] == "file" && len(parts) == 3 {
			target := resolveTarget(parts[1])
			sendFile(target, parts[2])
		} else {
			// Try to resolve name to IP, otherwise use raw input
			target := resolveTarget(parts[0])
			startChatClient(target)
		}
	}
}

// Helper to check if a string is a known peer name, otherwise return original string (IP)
func resolveTarget(input string) string {
	peerMutex.Lock()
	defer peerMutex.Unlock()
	if ip, exists := peerRegistry[input]; exists {
		return ip
	}
	return input
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
	buf := make([]byte, 1024)
	for {
		n, remoteAddr, _ := conn.ReadFromUDP(buf)
		msg := string(buf[:n])
		if strings.HasPrefix(msg, "IAM:") {
			peerName := msg[4:]
			ip := remoteAddr.IP.String()

			peerMutex.Lock()
			lastSeen, exists := discoveredPeers[ip]
			// Update registry
			peerRegistry[peerName] = ip

			if peerName != myHost && (!exists || time.Since(lastSeen) > 30*time.Second) {
				fmt.Printf("\n[Found Peer] Name: %s | IP: %s", peerName, ip)
				fmt.Print("\n> ")
			}
			discoveredPeers[ip] = time.Now()
			peerMutex.Unlock()
		}
	}
}

// --- TCP SERVER ---

func startTCPServer() {
	ln, err := net.Listen("tcp", ":"+portTCP)
	if err != nil {
		fmt.Printf("Server Error: %v\n", err)
		return
	}
	for {
		conn, _ := ln.Accept()
		go handleIncoming(conn)
	}
}

func handleIncoming(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	header, _ := reader.ReadString('\n')
	header = strings.TrimSpace(header)

	// Note: In a real terminal app, Scanln here conflicts with the main loop.
	// For this simple version, we assume the user responds to the prompt.
	fmt.Printf("\n[Incoming %s] Accept? (y/n): ", header)

	// Use a dedicated scanner for the response to avoid issues
	var response string
	fmt.Scanln(&response)

	if strings.ToLower(response) != "y" {
		fmt.Fprintln(conn, "REJECTED")
		return
	}
	fmt.Fprintln(conn, "ACCEPTED")

	if strings.HasPrefix(header, "FILE:") {
		fileName := strings.TrimPrefix(header, "FILE:")
		f, _ := os.Create("received_" + fileName)
		defer f.Close()
		io.Copy(f, reader)
		fmt.Printf("\n[Success] Saved received_%s\n> ", fileName)
	} else {
		fmt.Println("--- Chat Started (Type '.exit' to stop) ---")
		done := make(chan struct{})
		go func() {
			io.Copy(os.Stdout, reader)
			fmt.Println("\n[Peer disconnected]")
			close(done)
		}()

		// In-line chat loop
		inputScanner := bufio.NewScanner(os.Stdin)
		for inputScanner.Scan() {
			text := inputScanner.Text()
			if text == ".exit" {
				break
			}
			fmt.Fprintln(conn, text)
		}
		fmt.Println("--- Chat Ended ---")
	}
}

// --- CLIENTS ---

func startChatClient(ip string) {
	conn, err := net.DialTimeout("tcp", ip+":"+portTCP, 5*time.Second)
	if err != nil {
		fmt.Printf("Connection failed: %v\n", err)
		return
	}
	defer conn.Close()

	fmt.Fprintf(conn, "CHAT_REQUEST\n")

	respReader := bufio.NewReader(conn)
	status, _ := respReader.ReadString('\n')
	if !strings.Contains(status, "ACCEPTED") {
		fmt.Println("Connection rejected.")
		return
	}

	fmt.Println("Connected! Type your message (Type '.exit' to stop):")

	go io.Copy(os.Stdout, respReader)

	inputScanner := bufio.NewScanner(os.Stdin)
	for inputScanner.Scan() {
		text := inputScanner.Text()
		if text == ".exit" {
			break
		}
		fmt.Fprintln(conn, text)
	}
}

func sendFile(ip string, path string) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Println("File error:", err)
		return
	}
	defer file.Close()

	conn, err := net.DialTimeout("tcp", ip+":"+portTCP, 5*time.Second)
	if err != nil {
		fmt.Println("Dial error:", err)
		return
	}
	defer conn.Close()

	// Get just the filename if a full path was provided
	fInfo, _ := file.Stat()
	fmt.Fprintf(conn, "FILE:%s\n", fInfo.Name())

	respReader := bufio.NewReader(conn)
	status, _ := respReader.ReadString('\n')
	if !strings.Contains(status, "ACCEPTED") {
		fmt.Println("File transfer rejected.")
		return
	}

	io.Copy(conn, file)
	fmt.Println("File sent successfully.")
}
