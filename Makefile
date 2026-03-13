# Makefile for HyperBot cross-compilation

.PHONY: all clean build-cross build-windows build-linux build-macos help

# 默认目标
all: build-cross

# 交叉编译所有平台
build-cross:
	@echo "Cross-compiling for all platforms..."
	@if [ -f "./build-cross.ps1" ]; then \
		powershell -ExecutionPolicy Bypass -File "./build-cross.ps1"; \
	elif [ -f "./build-cross.sh" ]; then \
		chmod +x ./build-cross.sh && ./build-cross.sh; \
	else \
		echo "No cross-compilation script found"; \
	fi

# 仅编译Windows版本
build-windows:
	@echo "Building for Windows x64..."
	@env GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o release/windows-x64/HyperBot.exe

# 仅编译Linux版本
build-linux:
	@echo "Building for Linux x64..."
	@env GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o release/linux-x64/HyperBot
	@echo "Building for Linux ARM64..."
	@env GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o release/linux-arm64/HyperBot

# 仅编译macOS版本
build-macos:
	@echo "Building for macOS x64..."
	@env GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o release/macos-x64/HyperBot
	@echo "Building for macOS ARM64..."
	@env GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o release/macos-arm64/HyperBot

# 清理构建文件
clean:
	@echo "Cleaning build files..."
	@rm -f release/linux-x64/HyperBot \
		release/linux-arm64/HyperBot \
		release/macos-x64/HyperBot \
		release/macos-arm64/HyperBot \
		release/windows-x64/HyperBot.exe
	@echo "Clean complete."

# 显示帮助信息
help:
	@echo "Available targets:"
	@echo "  all/build-cross   - Build for all platforms (default)"
	@echo "  build-windows     - Build only for Windows x64"
	@echo "  build-linux       - Build only for Linux (x64 and ARM64)"
	@echo "  build-macos       - Build only for macOS (x64 and ARM64)"
	@echo "  clean             - Remove all built binaries"
	@echo "  help              - Show this help message"
	@echo ""
	@echo "Usage examples:"
	@echo "  make              # Build for all platforms"
	@echo "  make build-windows # Build only Windows version"
	@echo "  make clean        # Clean all built files"