# Windows 构建脚本 - 在 PowerShell 中运行
$ErrorActionPreference = "Stop"

$OutputDir = "release"
$LDFLAGS = "-s -w"

Write-Host "构建 windows-x64..." -ForegroundColor Yellow

# 确保输出目录存在
if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir | Out-Null
}

$env:CGO_ENABLED = "1"
go build -ldflags $LDFLAGS -o "$OutputDir/HyperBot.exe"
if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host "构建完成: $OutputDir/HyperBot.exe" -ForegroundColor Green

# 显示构建产物
Write-Host "`n构建产物:" -ForegroundColor Cyan
Get-Item "$OutputDir/HyperBot.exe" | Select-Object FullName, Length
