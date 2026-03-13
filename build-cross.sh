#!/bin/bash

# Shell 交叉编译脚本
# 为所有支持的平台编译 HyperBot

echo -e "\033[32m开始交叉编译 HyperBot...\033[0m"

# 定义目标平台
declare -A targets=(
    ["linux-x64"]="GOOS=linux GOARCH=amd64"
    ["linux-arm64"]="GOOS=linux GOARCH=arm64"
    ["macos-x64"]="GOOS=darwin GOARCH=amd64"
    ["macos-arm64"]="GOOS=darwin GOARCH=arm64"
    ["windows-x64"]="GOOS=windows GOARCH=amd64"
)

# 清理旧的构建文件
echo -e "\n\033[33m清理旧的构建文件...\033[0m"
for dir in "${!targets[@]}"; do
    output_file="release/$dir/HyperBot"
    if [[ "$dir" == "windows-x64" ]]; then
        output_file="release/$dir/HyperBot.exe"
    fi
    
    if [ -f "$output_file" ]; then
        rm -f "$output_file"
        echo "已删除: $output_file"
    fi
done

# 为每个目标平台编译
for dir in "${!targets[@]}"; do
    echo -e "\n\033[36m编译: $dir\033[0m"
    
    # 设置输出路径
    output_file="release/$dir/HyperBot"
    if [[ "$dir" == "windows-x64" ]]; then
        output_file="release/$dir/HyperBot.exe"
    fi
    
    # 构建命令
    build_cmd="env ${targets[$dir]} go build -trimpath -ldflags=\"-s -w\" -o \"$output_file\""
    
    echo "执行: $build_cmd"
    
    # 执行构建
    eval $build_cmd
    
    if [ $? -eq 0 ]; then
        echo -e "\033[32m✓ 成功: $output_file\033[0m"
        
        # 显示文件大小
        if [ -f "$output_file" ]; then
            file_size=$(du -h "$output_file" | cut -f1)
            echo "  大小: $file_size"
        fi
    else
        echo -e "\033[31m✗ 失败: $output_file\033[0m"
    fi
done

echo -e "\n\033[32m交叉编译完成!\033[0m"
echo -e "\n\033[33m生成的文件:\033[0m"

# 显示生成的文件
for dir in "${!targets[@]}"; do
    output_file="release/$dir/HyperBot"
    if [[ "$dir" == "windows-x64" ]]; then
        output_file="release/$dir/HyperBot.exe"
    fi
    
    if [ -f "$output_file" ]; then
        file_size=$(du -h "$output_file" | cut -f1)
        echo "  $output_file ($file_size)"
    fi
done

echo -e "\n\033[33m平台说明:\033[0m"
echo -e "  \033[90mlinux-x64:      Linux 64位 (x86_64)\033[0m"
echo -e "  \033[90mlinux-arm64:    Linux ARM64 (如树莓派4)\033[0m"
echo -e "  \033[90mmacos-x64:      macOS Intel 64位\033[0m"
echo -e "  \033[90mmacos-arm64:    macOS Apple Silicon (M1/M2/M3)\033[0m"
echo -e "  \033[90mwindows-x64:    Windows 64位 (.exe)\033[0m"