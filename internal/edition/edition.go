// Package edition selects the features available in this process.
package edition

import (
	"fmt"
	"os"
	"testing"
)

type Mode string

const (
	testDefaults Mode = ""
	Server       Mode = "enterprise-server"
	Desktop      Mode = "enterprise-desktop"
)

// defaultMode can be set at build time for a dedicated executable.
var defaultMode string

func Current() Mode {
	if mode := os.Getenv("MAGPIE_EDITION"); mode != "" {
		return Mode(mode)
	}
	if defaultMode != "" {
		return Mode(defaultMode)
	}
	if testing.Testing() {
		return testDefaults
	}
	return Server
}

func Validate() error {
	switch mode := Current(); mode {
	case Server, Desktop:
		return nil
	default:
		return fmt.Errorf("MAGPIE_EDITION: unknown edition %q (use enterprise-server or enterprise-desktop)", mode)
	}
}

func Enterprise() bool { return Current() != testDefaults }

func Builtin(id string) bool {
	switch Current() {
	case testDefaults:
		return true
	case Server:
		switch id {
		case "gateway", "routing", "groups", "budget", "rotation", "usage", "redact":
			return true
		}
	case Desktop:
		return id == "gateway" || id == "usage"
	}
	return false
}
