package commands

import (
	"fmt"
	"net"
)

// pickFreePort returns the first TCP port at or after start that can be bound
// on 127.0.0.1. It tries start, start+1, ... up to start+99 (100 candidates).
//
// Note: there is an inherent TOCTOU race between this check and the eventual
// bind. In practice the window is narrow (~1 s until Dolt binds the port) and
// the main collision scenario we defend against is steady-state: two projects
// permanently occupying the same port at the same time.
func pickFreePort(start int) (int, error) {
	for p := start; p < start+100; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			continue
		}
		_ = l.Close()
		return p, nil
	}
	return 0, fmt.Errorf("no free port in [%d, %d)", start, start+100)
}
