package mission

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"time"
)

// Event kinds written to log.jsonl.
const (
	EventCreated       = "created"
	EventCapsuleAdded  = "capsule_added"
	EventCapsuleRemove = "capsule_removed"
	EventCapsuleBurned = "capsule_burned"
	EventNote          = "note"
	EventCrewStarted   = "crew_started"
	EventCrewEnded     = "crew_ended"
	EventCrewResumed   = "crew_resumed"
	EventCrewAsked     = "crew_asked"
	EventCrewDone      = "crew_done"
	EventArtifact      = "artifact"
	EventState         = "state"
)

// Event is one line of a mission's log.
type Event struct {
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	Repo    string    `json:"repo,omitempty"`
	Capsule string    `json:"capsule,omitempty"`
	Crew    string    `json:"crew,omitempty"`
	Text    string    `json:"text,omitempty"`
}

// AppendLog adds an event to the mission's log. Time defaults to now, and
// Crew defaults to $WS_CREW so events from crew are attributed to them.
//
// Each event is a single O_APPEND write, so concurrent writers interleave
// whole lines rather than corrupting each other.
func (s Store) AppendLog(slug string, e Event) error {
	if !validSlug(slug) {
		return ErrNotFound
	}
	if e.Time.IsZero() {
		e.Time = now()
	}
	if e.Crew == "" {
		e.Crew = os.Getenv(EnvCrew)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.logPath(slug), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// ReadLog returns the mission's events, oldest first. If limit > 0 only the
// most recent limit events are returned. Malformed lines are skipped.
func (s Store) ReadLog(slug string, limit int) ([]Event, error) {
	f, err := os.Open(s.logPath(slug))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			events = append(events, e)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}
