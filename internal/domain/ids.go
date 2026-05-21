package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// ID prefixes used across services. Concentrating these here keeps generated
// identifiers consistent between the CLI, API, and storage adapters.
const (
	PrefixRepository   = "repo"
	PrefixSetting      = "set"
	PrefixSession      = "sess"
	PrefixSessionEvent = "sev"
	PrefixTask         = "task"
	PrefixWorkingState = "ws"
	PrefixUser         = "usr"
)

// NewID returns a random opaque ID with the given prefix. It falls back to a
// nanosecond timestamp if the system RNG is unavailable.
func NewID(prefix string) ID {
	var value [8]byte
	if _, err := rand.Read(value[:]); err == nil {
		return ID(prefix + "_" + hex.EncodeToString(value[:]))
	}
	return ID(fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()))
}

// DeterministicID returns a stable ID derived from the given inputs. Multiple
// inputs are joined with NUL so independent value spaces cannot collide.
func DeterministicID(prefix string, parts ...string) ID {
	value := ""
	for i, part := range parts {
		if i > 0 {
			value += "\x00"
		}
		value += part
	}
	sum := sha256.Sum256([]byte(value))
	return ID(prefix + "_" + hex.EncodeToString(sum[:])[:16])
}

// ChildID composes a deterministic child ID under a parent.
func ChildID(parent ID, kind string, index int) ID {
	return ID(fmt.Sprintf("%s_%s_%d", parent, kind, index))
}
