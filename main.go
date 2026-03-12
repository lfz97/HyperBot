package main

import (
	"os"
	"os/signal"
	"syscall"
	"trpcagent/bootstrap"
)

// 全局捕获ctrl+c信号，当捕获到信号时，向sigChan发送信号通知
func catchSignal() chan os.Signal {
	sigChan := make(chan os.Signal, 1)
	// 全局捕获 Ctrl+C (SIGINT) 和 Ctrl+\ (SIGQUIT)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGQUIT)
	return sigChan
}

func main() {
	//-----埋点测试------------------
	/*
		f, err := os.Create("trace.out")
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()

		err = trace.Start(f)
		if err != nil {
			log.Fatal(err)
		}
		defer trace.Stop()
	*/
	//----------------------------
	bootstrap.Boot(catchSignal())
}
