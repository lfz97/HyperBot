@echo off
echo ========================================
echo   HyperBot Cross-Compilation Script
echo ========================================
echo.

echo Building for Linux x64...
set GOOS=linux
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o release/linux-x64/HyperBot
set GOOS=
set GOARCH=

echo Building for Linux ARM64...
set GOOS=linux
set GOARCH=arm64
go build -trimpath -ldflags="-s -w" -o release/linux-arm64/HyperBot
set GOOS=
set GOARCH=

echo Building for macOS x64...
set GOOS=darwin
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o release/macos-x64/HyperBot
set GOOS=
set GOARCH=

echo Building for macOS ARM64...
set GOOS=darwin
set GOARCH=arm64
go build -trimpath -ldflags="-s -w" -o release/macos-arm64/HyperBot
set GOOS=
set GOARCH=

echo Building for Windows x64...
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o release/windows-x64/HyperBot.exe
set GOOS=
set GOARCH=

echo.
echo ========================================
echo   Cross-compilation completed!
echo ========================================
echo.
echo Generated files:
echo   release/linux-x64/HyperBot
echo   release/linux-arm64/HyperBot
echo   release/macos-x64/HyperBot
echo   release/macos-arm64/HyperBot
echo   release/windows-x64/HyperBot.exe
echo.
pause