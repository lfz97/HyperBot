# PowerShell 交叉编译脚本
# 为所有支持的平台编译 HyperBot

Write-Host "开始交叉编译 HyperBot..." -ForegroundColor Green

# 定义目标平台
$targets = @(
    @{GOOS="linux"; GOARCH="amd64"; OUTPUT="release/linux-x64/HyperBot"},
    @{GOOS="linux"; GOARCH="arm64"; OUTPUT="release/linux-arm64/HyperBot"},
    @{GOOS="darwin"; GOARCH="amd64"; OUTPUT="release/macos-x64/HyperBot"},
    @{GOOS="darwin"; GOARCH="arm64"; OUTPUT="release/macos-arm64/HyperBot"},
    @{GOOS="windows"; GOARCH="amd64"; OUTPUT="release/windows-x64/HyperBot.exe"}
)

# 清理旧的构建文件
Write-Host "清理旧的构建文件..." -ForegroundColor Yellow
foreach ($target in $targets) {
    if (Test-Path $target.OUTPUT) {
        Remove-Item $target.OUTPUT -Force
        Write-Host "已删除: $($target.OUTPUT)" -ForegroundColor Gray
    }
}

# 为每个目标平台编译
foreach ($target in $targets) {
    Write-Host "`n编译: GOOS=$($target.GOOS) GOARCH=$($target.GOARCH)" -ForegroundColor Cyan
    
    # 设置环境变量
    $env:GOOS = $target.GOOS
    $env:GOARCH = $target.GOARCH
    
    # 构建命令
    $buildCmd = "go build -trimpath -ldflags=`"-s -w`" -o `"$($target.OUTPUT)`""
    
    Write-Host "执行: $buildCmd" -ForegroundColor Gray
    
    # 执行构建
    $result = Invoke-Expression $buildCmd
    
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ 成功: $($target.OUTPUT)" -ForegroundColor Green
        
        # 如果是Windows可执行文件，显示文件信息
        if ($target.OUTPUT -like "*.exe") {
            $fileInfo = Get-Item $target.OUTPUT
            Write-Host "  大小: $([math]::Round($fileInfo.Length/1MB, 2)) MB" -ForegroundColor Gray
        }
    } else {
        Write-Host "✗ 失败: $($target.OUTPUT)" -ForegroundColor Red
    }
    
    # 清理环境变量
    Remove-Item Env:\GOOS
    Remove-Item Env:\GOARCH
}

Write-Host "`n交叉编译完成!" -ForegroundColor Green
Write-Host "`n生成的文件:" -ForegroundColor Yellow

# 显示生成的文件
foreach ($target in $targets) {
    if (Test-Path $target.OUTPUT) {
        $file = Get-Item $target.OUTPUT -ErrorAction SilentlyContinue
        if ($file) {
            $size = [math]::Round($file.Length/1MB, 2)
            Write-Host "  $($target.OUTPUT) ($size MB)" -ForegroundColor Gray
        }
    }
}

Write-Host "`n平台说明:" -ForegroundColor Yellow
Write-Host "  linux-x64:      Linux 64位 (x86_64)" -ForegroundColor Gray
Write-Host "  linux-arm64:    Linux ARM64 (如树莓派4)" -ForegroundColor Gray
Write-Host "  macos-x64:      macOS Intel 64位" -ForegroundColor Gray
Write-Host "  macos-arm64:    macOS Apple Silicon (M1/M2/M3)" -ForegroundColor Gray
Write-Host "  windows-x64:    Windows 64-bit (.exe)" -ForegroundColor Gray