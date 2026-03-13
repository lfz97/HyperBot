package main

import (
	"HyperBot/bootstrap"
	"os"
	"os/signal"
	"syscall"
)

// 全局捕获ctrl+c信号，当捕获到信号时，向sigChan发送信号通知
func catchSignal() chan os.Signal {
	sigChan := make(chan os.Signal)
	// 全局捕获 Ctrl+C (SIGINT) 和 Ctrl+\ (SIGQUIT)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGQUIT)
	return sigChan
}

func main() {

	bootstrap.Boot(catchSignal())
}
