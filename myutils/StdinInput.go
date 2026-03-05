package myutils

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// 通过bufio方式从标准输入读取用户输入，返回字符串和错误
func StdinInput(prompt string) (string, error) {
	//输出提示信息
	fmt.Println(prompt)

	//创建一个关联stdin的reader对象
	reader := bufio.NewReader(os.Stdin)

	//从stdin读取，阻塞直到用户输入换行符
	userInput, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	// 去除首尾空白字符（包括Windows的\r\n）,Windows一定要加
	trimmedInput := strings.TrimSpace(userInput)
	return trimmedInput, nil
}
