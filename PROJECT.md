
# PROJECT.md

This file provides guidance to AI agents when working with code in this repository.

# Project Overview

LAN-CHAT is a local Wi-Fi chat application built in Go that enables peer-to-peer communication, file sharing, and chat functionality within the same network without requiring internet connectivity.

## Project Tracking

**Always keep it these files up to date**

Before AND After doing work!!

`docs/TODO.md` tracks bugs, open items, and planned work.
When completing a task, fixing a bug, or discovering new work, update `docs/TODO.md` to reflect the current state.

`docs/plans/{FEATURE NAME}.md` Once a PLAN is devised added to the TODO fil Features section and save the plan in the folder.


## Architecture

### Core Components
- **Main Application** (`main.go`): Single-file implementation using Bubble Tea TUI framework
- **Network Layer**: Dual protocol approach with UDP for discovery and TCP for data transfer
- **UI States**: Multi-state interface (peer list, file picker, progress, chat)

### Network Architecture
- **UDP Broadcasting** (Port 9999): Peer discovery via broadcast to `255.255.255.255`
- **TCP Server** (Port 8080): Handles file transfers and chat messages
- **Concurrent Operations**: Separate goroutines for broadcasting, listening, and server operations

## Key Technologies

- **Go 1.25.3**: Core language
- **Bubble Tea v1.3.10**: TUI framework for terminal interface
- **Charmbracelet Bubbles**: UI components (list, filepicker, progress, textinput, viewport)
- **Lipgloss v1.1.0**: Styling and layout for terminal UI
- **Standard Library**: `net`, `os`, `sync`, `time`, `bufio`, `io`, `crypto/aes`, `crypto/cipher`, `crypto/rand`, `crypto/sha256`, `crypto/subtle`, `encoding/base64`, `encoding/hex`, `flag`

## Development Workflow

### Building and Running
```bash
go run main.go <username>
go run main.go --pass="secret" <username>   # encrypted mode
```
The application requires a username argument. The optional `--pass` flag enables AES-256-GCM encryption for chat and file transfers between peers sharing the same password.

### Testing
- Manual testing required for network functionality
- Test peer discovery by running multiple instances on different machines
- Verify file transfers and chat functionality

### Git Workflow
- **Remote Access**: Uses SSH for repository access (`git@github.com:HoldenMorris/LAN-CHAT.git`)
- **Commits**: Follow conventional commits (e.g., `feat:`, `fix:`, `refactor:`, `docs:`)
- **Pushing**: Ensure SSH keys are configured before pushing changes

## Code Conventions

### Go Style
- Standard Go formatting (`gofmt`)
- Package-level constants for ports
- Struct-based message passing for Bubble Tea
- Error handling with deferred connections

### Naming Conventions
- Lowercase camelCase for variables and functions
- Descriptive names (e.g., `peerUpdateMsg`, `transferStatusMsg`)
- Single-letter abbreviations only for common patterns (`fp`, `ti`, `cmd`)

### File Organization
- Single-file architecture (`main.go`)
- Clear section comments (`--- Messages ---`, `--- Model ---`, `--- Update ---`, `--- Networking ---`)
- Logical grouping of related functionality

## File Structure

```
LAN-CHAT/
├── main.go              # Complete application source
├── go.mod               # Go module definition
├── go.sum               # Dependency checksums
├── README.md            # Documentation and Bubble Tea guide
├── .gitignore           # Ignore built binary and received files
└── PROJECT.md           # This file
```

### Generated Files
- `lan-chat`: Compiled binary (ignored by git)
- `received_*.txt`: Files received from peers (ignored by git)

## Important Context

### State Management
The application uses a state-based UI model:
- **State 0**: Peer list (main view)
- **State 1**: File picker for selecting files to send
- **State 2**: Progress indicator during file transfer
- **State 3**: Chat interface with selected peer

### Network Protocol
- **Discovery**: `IAM:<username>` broadcast via UDP every 3 seconds
- **File Transfer**: `FILE:<filename>` header followed by file content
- **Chat Messages**: `CHAT:<sender>:<message>` format
- **Encrypted Chat**: `ECHAT:<sender>:<base64-encrypted>` — AES-256-GCM encrypted messages
- **Encrypted File**: `EFILE:<filename>` header followed by base64-encoded encrypted content
- **Password Verify**: `VERIFY:<fingerprint>` handshake on TCP, responds `VMATCH` or `VNOMATCH`

### Key Functions
- `initialModel()`: Initializes the TUI model with username, password, and network channel
- `broadcast()`: Continuously broadcasts presence via UDP
- `listenUDP()`: Listens for peer discovery messages, triggers password verification
- `startTCPServer()`: Handles incoming TCP connections for files, chat, and password verification
- `sendFileCmd()` / `sendChatCmd()`: Initiate outbound transfers (encrypted if peer verified)
- `verifyPeer()`: TCP handshake to check if remote peer shares the same password
- `encryptData()` / `decryptData()`: AES-256-GCM encryption/decryption helpers
- `passwordFingerprint()`: Generates a verification hash from password (never reveals password)

### Dependencies
The project uses minimal external dependencies, focusing on the Charmbracelet ecosystem for terminal UI components. All networking is handled using Go's standard library.

## Security Considerations

- Optional AES-256-GCM encryption via `--pass` flag for chat and file transfers
- Password verification uses SHA-256 fingerprint exchange (password never sent over network)
- Fingerprint comparison uses constant-time comparison to prevent timing attacks
- Files are received with `received_` prefix to prevent overwrites
- TCP connections have 2-second timeout for chat messages
- Network discovery limited to local broadcast domain
- Without `--pass`, all communication remains unencrypted (backward compatible)