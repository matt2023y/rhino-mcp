param([string]$Yak = "$env:ProgramFiles\Rhino 8\System\Yak.exe")
$ErrorActionPreference = 'Stop'
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
    foreach ($command in @('make', 'sh', 'zip', 'mktemp', 'mkdir', 'cp', 'chmod', 'mv', 'rm')) {
        if (-not (Get-Command $command -CommandType Application -ErrorAction SilentlyContinue)) { throw "Required packaging command not found: $command (install MSYS2 tools and add them to PATH)" }
    }
    & "$PSScriptRoot/build.ps1" -Architecture amd64
    & dotnet build plugin/RhinoMcpPlugin.csproj -c Release
    if ($LASTEXITCODE -ne 0) { throw 'Rhino plugin build failed' }
    $yakExecutable = (Resolve-Path $Yak).Path
    $stage = Join-Path (Get-Location) ('.build/yak-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force $stage | Out-Null
    try {
        Copy-Item plugin/manifest.yml, plugin/bin/Release/net7.0/RhinoMcpPlugin.rhp, plugin/bin/Release/net7.0/RhinoMcpPlugin.deps.json $stage
        Push-Location $stage
        try {
            & $yakExecutable build --platform any
            if ($LASTEXITCODE -ne 0) { throw 'Yak packaging failed' }
        } finally { Pop-Location }
        New-Item -ItemType Directory -Force dist/yak | Out-Null
        Copy-Item "$stage/*.yak" dist/yak
        if (Test-Path .build/yak-package) { Remove-Item -Recurse -Force .build/yak-package }
        New-Item -ItemType Directory .build/yak-package | Out-Null
        Copy-Item "$stage/*.yak" .build/yak-package
    } finally { Remove-Item -Recurse -Force $stage }
    & make SHELL=sh package-assemble-windows
    if ($LASTEXITCODE -ne 0) { throw 'Skill packaging failed' }
} finally {
    Pop-Location
}
