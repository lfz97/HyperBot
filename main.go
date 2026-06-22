package main

import (
	"HyperBot/bootstrap"
	"HyperBot/global"
)

func main() {
	global.Frontendinit()
	global.Backendinit(func() { bootstrap.Init("HyperBot") },
		func() { bootstrap.AgentStart() })

	global.TuiRun()
}
