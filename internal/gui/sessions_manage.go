package gui

import "github.com/yetone/magpie/internal/agent"

func agentLooks() map[string]agent.Look {
	out := map[string]agent.Look{}
	for _, a := range agent.Looks() {
		out[a.ID] = a
	}
	return out
}

// forgetStats has the Usage page's session stats read again: a session
// deleted or restored changes them.
func forgetStats() {
	statsMemo.Lock()
	statsMemo.m = nil
	statsMemo.Unlock()
}
