package main

import (
	"HyperBot/bootstrap"
	"HyperBot/global"
)

func main() {
	global.TuiInit(
		func() { bootstrap.Init("HyperBot") },
		func() { bootstrap.AgentStart() },
	)
	global.TuiRun()
}
