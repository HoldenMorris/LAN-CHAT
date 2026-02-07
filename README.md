A local WIFI chat
===

Chat and send files to others on the same WIFI network.

## Installation

### Prerequisites
- Go 1.25.3 or later

### Build from source
```bash
git clone https://github.com/HoldenMorris/LAN-CHAT.git
cd LAN-CHAT
go mod tidy
go build -o lan-chat main.go
```

## Usage

### Basic usage
```bash
# Run with username
go run main.go <username>

# Or run the compiled binary
./lan-chat <username>
```

### Encrypted communication
```bash
# Run with password for AES-256-GCM encryption
go run main.go --pass="your-secret-password" <username>

# All peers must use the same password to communicate
```

### Features
- **Peer Discovery**: Automatically finds other users on the same WiFi network
- **File Transfer**: Send files directly to peers
- **Chat**: Real-time messaging with optional encryption
- **Terminal UI**: Clean, intuitive interface using Bubble Tea

### Network requirements
- All users must be on the same local network/WiFi
- UDP port 9999 for peer discovery
- TCP port 8080 for file transfers and chat

### Controls
- Use arrow keys to navigate
- Enter to select peers/files
- Tab to switch between chat input and file selection
- Ctrl+C to exit