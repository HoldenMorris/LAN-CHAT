package main

import (
	"bufio"
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

// Global map to track discovered peers and prevent spam
var (
	discoveredPeers = make(map[string]time.Time)
	peerMutex       sync.Mutex
)

func main() {
	host, _ := os.Hostname()
	fmt.Printf("Starting Peer: %s\n", host)

	go listenForPeers(host)
	go broadcastIdentity(host)
	go startTCPServer()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !scanner.Scan() { break }
		input := scanner.Text()

		parts := strings.Split(input, " ")
		if parts[0] == "exit" {
			break
		} else if parts[0] == "file" && len(parts) == 3 {
			sendFile(parts[1], parts[2])
		} else if parts[0] != "" {
			startChatClient(parts[0])
		}
	}
}

// --- DISCOVERY WITH SPAM FILTER ---

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
			name := msg[4:]
			ip := remoteAddr.IP.String()

			peerMutex.Lock()
			lastSeen, exists := discoveredPeers[ip]
			// Only print if new OR haven't seen in 30 seconds
			if name != myHost && (!exists || time.Since(lastSeen) > 30*time.Second) {
				fmt.Printf("\n[New Peer Found] %s (%s)", name, ip)
				fmt.Print("\n> ") // Re-print prompt
			}
			discoveredPeers[ip] = time.Now()
			peerMutex.Unlock()
		}
	}
}

// --- TCP SERVER WITH ACCEPT/REJECT ---

func startTCPServer() {
	ln, _ := net.Listen("tcp", ":"+portTCP)
	for {
		conn, _ := ln.Accept()
		go handleIncoming(conn)
	}
}

func handleIncoming(conn net.Conn) {
	reader := bufio.NewReader(conn)
	header, _ := reader.ReadString('\n')
	header = strings.TrimSpace(header)

	// Ask for permission
	fmt.Printf("\n[Request] %s from %s. Accept? (y/n): ", header, conn.RemoteAddr())
	var response string
	fmt.Scanln(&response)

	if strings.ToLower(response) != "y" {
		fmt.Fprintln(conn, "REJECTED")
		conn.Close()
		return
	}
	fmt.Fprintln(conn, "ACCEPTED")

	if strings.HasPrefix(header, "FILE:") {
		fileName := strings.TrimPrefix(header, "FILE:")
		f, _ := os.Create("received_" + fileName)
		defer f.Close()
		io.Copy(f, reader)
		fmt.Printf("\n[Success] Saved received_%s\n", fileName)
	} else {
		// Chat mode
		go io.Copy(os.Stdout, reader)
		io.Copy(conn, os.Stdin)
	}
}

// --- CLIENTS ---

func startChatClient(ip string) {
	conn, err := net.Dial("tcp", ip+":"+portTCP)
	if err != nil {
		fmt.Println("Error:", err); return
	}
	defer conn.Close()

	fmt.Fprintf(conn, "CHAT_REQUEST\n")

	// Wait for acceptance
	respReader := bufio.NewReader(conn)
	status, _ := respReader.ReadString('\n')
	if !strings.Contains(status, "ACCEPTED") {
		fmt.Println("Connection rejected by peer.")
		return
	}

	fmt.Println("Connected! (Ctrl+C to end)")
	go io.Copy(os.Stdout, conn)
	io.Copy(conn, os.Stdin)
}

func sendFile(ip string, path string) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Println("File error:", err); return
	}
	defer file.Close()

	conn, _ := net.Dial("tcp", ip+":"+portTCP)
	defer conn.Close()

	fmt.Fprintf(conn, "FILE:%s\n", file.Name())

	respReader := bufio.NewReader(conn)
	status, _ := respReader.ReadString('\n')
	if !strings.Contains(status, "ACCEPTED") {
		fmt.Println("File transfer rejected.")
		return
	}

	io.Copy(conn, file)
	fmt.Println("File sent successfully.")
}