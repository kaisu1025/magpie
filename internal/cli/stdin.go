package cli

import (
	"bufio"
	"os"
	"sync"
)

// stdin is read through one reader, so a second secret piped in is the
// second line and not lost to the first one's buffer. The reader is made
// over os.Stdin as it is when first read, never at package init: package
// variables are set before any init runs, and on Windows the desktop
// build's init (console_windows.go) only then swaps in the terminal's
// CONIN$. A reader made earlier keeps the invalid handle, and every
// prompt fails with "read /dev/stdin: The handle is invalid".
var (
	stdinMu   sync.Mutex
	stdinFile *os.File
	stdinBuf  *bufio.Reader
)

func stdin() *bufio.Reader {
	stdinMu.Lock()
	defer stdinMu.Unlock()
	if stdinBuf == nil || stdinFile != os.Stdin {
		stdinFile, stdinBuf = os.Stdin, bufio.NewReader(os.Stdin)
	}
	return stdinBuf
}
