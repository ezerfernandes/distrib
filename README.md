# distrib

Distribute HTML files across your home computers over the local network.

No cloud, no accounts, no config files. Just a single binary on each machine.

## How it works

One command runs on each receiving machine (`distrib serve`), another sends files (`distrib push`). Peers discover each other automatically via UDP broadcast on the local network.

When a file arrives, the receiver:
- Stores it locally in `~/.distrib/files/`
- Shows an OS-native notification
- Updates the web UI in real time

## Install

Requires Go 1.22+.

```
go install github.com/ezerfernandes/distrib@latest
```

Or build from source:

```
git clone https://github.com/ezerfernandes/distrib.git
cd distrib
go build -o distrib .
```

### Release binaries

```
GOOS=darwin  GOARCH=arm64 go build -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty)" -o dist/distrib-darwin-arm64 .
GOOS=darwin  GOARCH=amd64 go build -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty)" -o dist/distrib-darwin-amd64 .
GOOS=linux   GOARCH=amd64 go build -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty)" -o dist/distrib-linux-amd64 .
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty)" -o dist/distrib-windows-amd64.exe .
```

No CGO — cross-compilation works out of the box. Binaries are output to `dist/` with the version embedded from the latest git tag.

## Server (receiver)

Start the receiver daemon on each machine that should receive files:

```
distrib serve
```

This starts:
- An HTTP server on port **9848** (receives files, serves the web UI)
- A UDP listener on port **9847** (responds to peer discovery)

Open **http://localhost:9848/files** in a browser to see received files. The page updates live as new files arrive.

### Flags

```
-port           HTTP port (default: 9848)
-discovery-port UDP discovery port (default: 9847)
-name           Machine name shown to senders (default: hostname)
-data           Data directory (default: ~/.distrib)
```

### Examples

```
# Use defaults
distrib serve

# Custom name and port
distrib serve -name living-room -port 8080

# Store files elsewhere
distrib serve -data /tmp/distrib-files
```

### Notifications

The server shows an OS notification when a file arrives:

| Platform | Method |
|----------|--------|
| Linux    | `notify-send` (install `libnotify` if missing) |
| macOS    | Native notification via `osascript` |
| Windows  | Balloon notification via PowerShell |

## Client (sender)

Push an HTML file to all discovered receivers:

```
distrib push report.html
```

The client broadcasts a UDP discovery packet, waits 2 seconds for responses, then sends the file to every receiver that replied.

### Flags

```
-target         Send directly to a specific host:port (skips discovery)
-discovery-port UDP discovery port (default: 9847)
-timeout        How long to wait for discovery responses (default: 2s)
```

### Examples

```
# Auto-discover receivers on the network
distrib push dashboard.html

# Send to a specific machine
distrib push report.html -target 192.168.1.50:9848

# Send to localhost (for testing)
distrib push test.html -target localhost:9848
```

### Output

```
Discovering peers...
Found 2 peer(s):
  1. living-room (192.168.1.50:9848)
  2. office-pc (192.168.1.51:9848)
Pushing report.html to living-room... OK (id: 20260226-153045-a1b2c3)
Pushing report.html to office-pc... OK (id: 20260226-153045-a1b2c3)
```

## WSL2 note

WSL2 in its default NAT networking mode uses a private virtual subnet. UDP broadcasts from WSL2 won't reach other machines on your WiFi.

Two options:

1. **Use `-target` to skip discovery:**
   ```
   distrib push page.html -target 192.168.1.50:9848
   ```

2. **Enable mirrored networking** in `%USERPROFILE%/.wslconfig`:
   ```ini
   [wsl2]
   networkingMode=mirrored
   ```
   Then restart WSL. Discovery will work normally.

## Storage

Received files are stored under `~/.distrib/files/`, one directory per file:

```
~/.distrib/files/
  20260226-153045-a1b2c3/
    original.html   # the file as received
    meta.json       # metadata (sender, timestamp, size, sha256)
```

## API

The server exposes a JSON API alongside the web UI:

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/receive` | Push a file (multipart form: `file` + `sender`) |
| `GET` | `/files` | List files (JSON with `Accept: application/json`, web UI otherwise) |
| `GET` | `/files/{id}` | File metadata (JSON) |
| `GET` | `/files/{id}/raw` | Serve the raw HTML file |
| `GET` | `/events` | SSE stream — emits `file-received` events |
| `GET` | `/health` | Health check (returns `{"name":"...","status":"ok"}`) |

## Ports

| Port | Protocol | Purpose |
|------|----------|---------|
| 9847 | UDP | Peer discovery |
| 9848 | TCP | HTTP server (file transfer + web UI) |

Both are configurable via flags.
