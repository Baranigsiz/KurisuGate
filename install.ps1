$Repo = "Baranigsiz/KurisuGate"
$Binary = "kurisugate.exe"

Write-Host "⚡ Installing KurisuGate for Windows..." -ForegroundColor Cyan

$Arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
$Release = try {
    (Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest").tag_name
} catch {
    "v1.0.0"
}

$ZipUrl = "https://github.com/$Repo/releases/download/$Release/kurisugate_windows_$Arch.zip"
$InstallDir = "$env:LOCALAPPDATA\Programs\KurisuGate"

if (!(Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

$ZipFile = "$env:TEMP\kurisugate.zip"
Invoke-WebRequest -Uri $ZipUrl -OutFile $ZipFile
Expand-Archive -Path $ZipFile -DestinationPath $InstallDir -Force
Remove-Item $ZipFile -Force

$UserPath = [Environment]::GetEnvironmentVariable("Path", [EnvironmentVariableTarget]::User)
if ($UserPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", [EnvironmentVariableTarget]::User)
    Write-Host "✔ Added $InstallDir to user PATH." -ForegroundColor Green
}

Write-Host "✔ KurisuGate $Release installed successfully to $InstallDir\$Binary!" -ForegroundColor Green
Write-Host "🚀 Run 'kurisugate start' in a new terminal window to launch." -ForegroundColor Yellow
