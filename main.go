package main

import (
	"HyperBot/bootstrap"
	"HyperBot/global"
)

func main() {
	global.PageCreate()

	global.AgentEngineRun(func() { bootstrap.Init("HyperBot") },
		func() { bootstrap.AgentStart() })

	global.TuiRun()
}
