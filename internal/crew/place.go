package crew

import (
	"errors"
	"fmt"
	"strings"
	"syscall"

	"github.com/brudil/workspace/internal/mission"
)

// Capsule is a mission capsule crew can work in.
type Capsule struct {
	Name string // repo/name
	Path string
}

// Place is where crew work.
type Place struct {
	Dir     string
	Capsule string // repo/name, or a label like "(briefing)"
	// Busy is the crew already working there, if any.
	Busy string
}

// ErrNoCapsule means the mission has no capsule to work in yet.
var ErrNoCapsule = errors.New("no capsule to work in")

// UnknownCapsuleError is a capsule asked for that the mission doesn't have.
type UnknownCapsuleError struct {
	Want string
	Have []string
}

func (e *UnknownCapsuleError) Error() string {
	if len(e.Have) == 0 {
		return "no capsule " + e.Want
	}
	return fmt.Sprintf("no capsule %s (it has: %s)", e.Want, strings.Join(e.Have, ", "))
}

// PickPlace chooses where crew work: the capsule asked for (want, as
// repo/name), else the first one nobody is busy in, else the first (with
// Busy set, so the caller can ask before sharing, or refuse). busy maps a
// capsule to the crew working in it.
func PickPlace(capsules []Capsule, busy map[string]string, want string) (Place, error) {
	var have []string
	var first *Place
	for _, c := range capsules {
		have = append(have, c.Name)
		p := Place{Dir: c.Path, Capsule: c.Name, Busy: busy[c.Name]}
		if want != "" {
			if c.Name == want {
				return p, nil
			}
			continue
		}
		if p.Busy == "" {
			return p, nil
		}
		if first == nil {
			first = &p
		}
	}
	switch {
	case want != "":
		return Place{}, &UnknownCapsuleError{Want: want, Have: have}
	case first != nil:
		return *first, nil
	}
	return Place{}, ErrNoCapsule
}

// BusyIn is which capsules crew are working in, by the mission's records:
// crew that haven't ended, and whose process is alive (or not started yet).
// A record whose process is gone (a crashed deck server) works nowhere.
func BusyIn(m *mission.Mission) map[string]string {
	busy := map[string]string{}
	for _, c := range m.Crew {
		if c.Ended == nil && c.Capsule != "" && (c.PID == 0 || ProcessAlive(c.PID)) {
			busy[c.Capsule] = c.ID
		}
	}
	return busy
}

// ProcessAlive reports whether pid is a running process.
func ProcessAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
