# Project Overview

LAN-CHAT is a local Wi-Fi chat application built in Go that enables peer-to-peer communication, file sharing, and chat functionality within the same network without requiring internet connectivity.

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
- **Standard Library**: `net`, `os`, `sync`, `time`, `bufio`, `io`

## Development Workflow

### Building and Running
```bash
go run main.go <username>
```
The application requires a username argument and builds the binary automatically.

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

### Key Functions
- `initialModel()`: Initializes the TUI model with username and network channel
- `broadcast()`: Continuously broadcasts presence via UDP
- `listenUDP()`: Listens for peer discovery messages
- `startTCPServer()`: Handles incoming TCP connections for files and chat
- `sendFileCmd()` / `sendChatCmd()`: Initiate outbound transfers

### Dependencies
The project uses minimal external dependencies, focusing on the Charmbracelet ecosystem for terminal UI components. All networking is handled using Go's standard library.

## Security Considerations

- No authentication or encryption implemented
- Files are received with `received_` prefix to prevent overwrites
- TCP connections have 2-second timeout for chat messages
- Network discovery limited to local broadcast domain