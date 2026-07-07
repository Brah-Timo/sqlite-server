# Installation & Build Guide
## Full Commands — PowerShell and Bash

---

## Prerequisites

| Tool | Minimum Version | Notes |
|------|----------------|-------|
| Go   | 1.22+          | https://go.dev/dl/ |
| Git  | any            | for cloning the repo |
| make | optional       | shortcut targets only |

> **Important — first-build time**: `modernc.org/sqlite` transpiles the entire
> SQLite C source (~150 000 lines) to Go the first time you build.  
> This takes **3–5 minutes** on a typical machine.  
> All subsequent builds use the module cache and finish in under 10 seconds.

---

## Clone and Basic Build

### Bash (Linux / macOS)

```bash
# 1. Clone
git clone https://github.com/sqlite-server/sqlite-server
cd sqlite-server

# 2. Download dependencies (first run only)
go mod download

# 3. Build the executable
CGO_ENABLED=0 go build -o sqlite-server ./cmd/sqlite-server

# 4. Verify
./sqlite-server version
# Output: sqlite-server dev

# 5. Quick start
./sqlite-server --no-auth -- myapp.db
```

### PowerShell (Windows)

```powershell
# 1. Clone
git clone https://github.com/sqlite-server/sqlite-server
cd sqlite-server

# 2. Download dependencies
go mod download

# 3. Build for Windows
$env:CGO_ENABLED = "0"
go build -o sqlite-server.exe .\cmd\sqlite-server

# 4. Verify
.\sqlite-server.exe version
# Output: sqlite-server dev

# 5. Quick start
.\sqlite-server.exe --no-auth -- myapp.db
```

### cmd.exe (Windows)

```cmd
set CGO_ENABLED=0
go build -o sqlite-server.exe .\cmd\sqlite-server
sqlite-server.exe version
```

---

## Building the Windows `.exe`

### From Linux or macOS (cross-compilation)

Go's built-in cross-compilation makes this trivial — no Windows machine needed.

```bash
# Windows x64
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -o sqlite-server-windows-amd64.exe ./cmd/sqlite-server

# Windows ARM64
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 \
  go build -o sqlite-server-windows-arm64.exe ./cmd/sqlite-server

# Verify the files
ls -lh sqlite-server-windows-*.exe
# -rwxr-xr-x ... sqlite-server-windows-amd64.exe  16M
# -rwxr-xr-x ... sqlite-server-windows-arm64.exe  15M
```

### From Windows — PowerShell

```powershell
# Build for the current machine (Windows x64)
$env:GOOS        = "windows"
$env:GOARCH      = "amd64"
$env:CGO_ENABLED = "0"
go build -o sqlite-server.exe .\cmd\sqlite-server

# Build with version information embedded
$VERSION    = (git describe --tags --always 2>$null) ?? "1.0.0"
$COMMIT     = (git rev-parse --short HEAD 2>$null) ?? "unknown"
$BUILD_DATE = (Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ")

go build `
  -ldflags "-s -w -X main.Version=$VERSION -X main.Commit=$COMMIT -X main.BuildDate=$BUILD_DATE" `
  -trimpath `
  -o sqlite-server.exe `
  .\cmd\sqlite-server

# Show the result
.\sqlite-server.exe version
Get-Item sqlite-server.exe | Select-Object Name, @{n='Size (MB)';e={[math]::Round($_.Length/1MB,1)}}
```

### From Windows — cmd.exe

```cmd
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go build -o sqlite-server.exe .\cmd\sqlite-server
sqlite-server.exe version
```

---

## Cross-Compile for All Platforms at Once

### Using the Makefile (Linux / macOS)

```bash
make build-all

# Output in dist/
# dist/sqlite-server-linux-amd64
# dist/sqlite-server-linux-arm64
# dist/sqlite-server-darwin-amd64
# dist/sqlite-server-darwin-arm64
# dist/sqlite-server-windows-amd64.exe
# dist/sqlite-server-freebsd-amd64
```

### Bash script (manual)

```bash
#!/bin/bash
set -e
MODULE="./cmd/sqlite-server"
mkdir -p dist

targets=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "freebsd/amd64"
)

for target in "${targets[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  outfile="dist/sqlite-server-${os}-${arch}"
  [[ "$os" == "windows" ]] && outfile="${outfile}.exe"

  echo -n "Building ${os}/${arch} ... "
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -o "$outfile" $MODULE
  echo "$(du -sh "$outfile" | cut -f1)"
done

echo ""
echo "All binaries:"
ls -lh dist/
```

### PowerShell script

```powershell
$MODULE = ".\cmd\sqlite-server"
New-Item -ItemType Directory -Force -Path dist | Out-Null

$targets = @(
  @{ OS="linux";   Arch="amd64" },
  @{ OS="linux";   Arch="arm64" },
  @{ OS="darwin";  Arch="amd64" },
  @{ OS="darwin";  Arch="arm64" },
  @{ OS="windows"; Arch="amd64" },
  @{ OS="freebsd"; Arch="amd64" }
)

foreach ($t in $targets) {
  $env:GOOS        = $t.OS
  $env:GOARCH      = $t.Arch
  $env:CGO_ENABLED = "0"

  $out = "dist\sqlite-server-$($t.OS)-$($t.Arch)"
  if ($t.OS -eq "windows") { $out += ".exe" }

  Write-Host "Building $($t.OS)/$($t.Arch) ..." -NoNewline
  go build -o $out $MODULE
  $size = [math]::Round((Get-Item $out).Length / 1MB, 1)
  Write-Host " ${size} MB"
}

# Reset environment
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue

Write-Host "`nAll binaries:"
Get-ChildItem dist\ | Format-Table Name, @{n='Size (MB)';e={[math]::Round($_.Length/1MB,1)}}
```

---

## Build Modes Compared

### Debug build (development)

```bash
# No size stripping — better stack traces
go build -o sqlite-server ./cmd/sqlite-server
```

### Release build (production)

```bash
# -s   strip symbol table
# -w   strip DWARF debug info
# -trimpath  remove local machine paths from the binary
CGO_ENABLED=0 go build \
  -ldflags "-s -w -X main.Version=v1.0.0" \
  -trimpath \
  -o sqlite-server \
  ./cmd/sqlite-server

# Compare sizes
# Debug:   ~25 MB
# Release: ~16 MB
```

### With full version metadata

```bash
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

CGO_ENABLED=0 go build \
  -ldflags "-s -w \
    -X main.Version=${VERSION} \
    -X main.Commit=${COMMIT} \
    -X main.BuildDate=${BUILD_DATE}" \
  -trimpath \
  -o sqlite-server \
  ./cmd/sqlite-server

./sqlite-server version
# sqlite-server v1.2.3-abc1234 (2026-07-07T11:00:00Z)
```

---

## Building a Docker Image

```bash
# Build
docker build -t sqlite-server:latest .

# Run
docker run -d \
  --name sqlite-server \
  -p 5432:5432 \
  -v "$(pwd)/data:/data" \
  sqlite-server:latest \
  --no-auth -- /data/myapp.db

# Check it
psql -h localhost -p 5432 -U test -c "SELECT 1"
```

---

## Module Commands Reference

```bash
# Download all dependencies to the local cache
go mod download

# Remove unused dependencies, add missing ones
GONOSUMDB="*" go mod tidy

# Verify checksums of every module in the cache
go mod verify
# all modules verified

# List direct and indirect dependencies
go list -m all

# Show dependency graph
go mod graph
```

---

## Common Build Errors

### `go: command not found`

```bash
# Linux — find the Go binary
find /usr /home /opt -name "go" -type f 2>/dev/null | grep "bin/go$"

export PATH=$PATH:/usr/local/go/bin        # add to current shell
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc   # persist
source ~/.bashrc
```

```powershell
# Windows PowerShell
$env:PATH = "$env:PATH;C:\Go\bin"
# or add C:\Go\bin to system PATH via System Properties
```

### `verifying module: checksum mismatch`

```bash
rm go.sum
GONOSUMDB="*" GOFLAGS="-mod=mod" go mod tidy
```

### `cannot find package`

```bash
cat go.mod | head -3
# module github.com/sqlite-server/sqlite-server

go clean -modcache
go mod download
```

### Build takes 5+ minutes

This is normal on the **first build only**.  
`modernc.org/sqlite` transpiles ~150 000 lines of C to Go.  
The compiled output is cached; every subsequent build takes < 10 seconds.

### `signal: killed` during `go vet ./...`

The sqlite library's generated file is so large it can exhaust RAM during static
analysis.  Vet your own packages explicitly:

```bash
go vet github.com/sqlite-server/sqlite-server/internal/pgproto
go vet github.com/sqlite-server/sqlite-server/sql/...
go vet github.com/sqlite-server/sqlite-server/internal/engine
go vet github.com/sqlite-server/sqlite-server/internal/catalog
```
