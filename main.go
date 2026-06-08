package main

import (
	"HyperBot/global"
	"HyperBot/tui"
)

func main() {
	tui.TuiInit()
	if err := global.App_p.Run(); err != nil {
		panic(err)
	}
}
