package boot

import (
	"HyperBot/service/engine"
	"HyperBot/service/tui"
)

func Boot() {
	tui := tui.GetTuiService()
	go func() {
		e := engine.GetEngineService("HyperBot", tui)
		e.AgentStart()
	}()
	tui.Run()

}
