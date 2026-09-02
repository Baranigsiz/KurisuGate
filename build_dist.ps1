param(
    [string]$Version = "v1.2.0"
)

$ErrorActionPreference = "Stop"

if (!(Test-Path "dist")) {
    New-Item -ItemType Directory -Path "dist" | Out-Null
}

$targets = @(
    @{ OS = "windows"; Arch = "amd64"; Binary = "kurisugate.exe"; Archive = "kurisugate_windows_amd64.zip" },
    @{ OS = "linux"; Arch = "amd64"; Binary = "kurisugate"; Archive = "kurisugate_linux_amd64.tar.gz" },
    @{ OS = "linux"; Arch = "arm64"; Binary = "kurisugate"; Archive = "kurisugate_linux_arm64.tar.gz" },
    @{ OS = "darwin"; Arch = "arm64"; Binary = "kurisugate"; Archive = "kurisugate_darwin_arm64.tar.gz" },
    @{ OS = "darwin"; Arch = "amd64"; Binary = "kurisugate"; Archive = "kurisugate_darwin_amd64.tar.gz" }
)

foreach ($t in $targets) {
    Write-Host "Building for $($t.OS)/$($t.Arch) (Version: $Version)..." -ForegroundColor Cyan
    $env:GOOS = $t.OS
    $env:GOARCH = $t.Arch
    $outPath = "dist/$($t.Binary)"
    
    go build -ldflags="-s -w -X main.Version=$Version" -o $outPath ./cmd/kurisu

    if ($t.Archive.EndsWith(".zip")) {
        Compress-Archive -Path $outPath, "README.md", "LICENSE", "config.example.yaml" -DestinationPath "dist/$($t.Archive)" -Force
    } else {
        tar -czf "dist/$($t.Archive)" -C dist "$($t.Binary)"
    }
}

$env:GOOS = ""
$env:GOARCH = ""
Write-Host "All targets built successfully!" -ForegroundColor Green
