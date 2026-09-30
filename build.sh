#!/bin/bash
# Linux 构建脚本
set -e

OUTPUT_DIR="release"
LDFLAGS="-s -w"

echo -e "\033[33m构建 linux-x64...\033[0m"

# 先跑 .gsx 代码生成（幂等：模板没改时输出零差异），
# 防止"改了模板忘跑 go generate、构建静默用旧视图"——生成器版本由 go.mod 钉住
(cd service/tui && go generate ./...)

mkdir -p "$OUTPUT_DIR"
CGO_ENABLED=1 go build -ldflags "$LDFLAGS" -o "$OUTPUT_DIR/HyperBot" ./cmd

echo -e "\033[32m构建完成: $OUTPUT_DIR/HyperBot\033[0m"
echo ""
echo "构建产物:"
ls -lh "$OUTPUT_DIR/HyperBot"
