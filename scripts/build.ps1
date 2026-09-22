param([ValidateSet('amd64')][string]$Architecture = 'amd64')
$ErrorActionPreference = 'Stop'
$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
$previousCGO = $env:CGO_ENABLED
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
    $env:GOOS = 'windows'
    $env:GOARCH = $Architecture
    $env:CGO_ENABLED = '0'
    & go build -trimpath '-ldflags=-s -w' -o .build/windows/rhino-mcp-server.exe ./cmd/rhino-mcp-server
    if ($LASTEXITCODE -ne 0) { throw 'MCP server build failed' }
    & go build -trimpath '-ldflags=-s -w' -o .build/windows/rhino-tool.exe ./cmd/rhino-tool
    if ($LASTEXITCODE -ne 0) { throw 'Tool build failed' }
} finally {
    $env:GOOS = $previousGOOS
    $env:GOARCH = $previousGOARCH
    $env:CGO_ENABLED = $previousCGO
    Pop-Location
}
