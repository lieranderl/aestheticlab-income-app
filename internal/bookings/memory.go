package bookings

import (
	"context"
	"sync"
	"time"
)

// Memory is an in-process Source for tests, e2e and local runs.
type Memory struct {
	mu     sync.Mutex
	events map[string]map[string]Event // calendar → id → event
	fail   error
	gets   int
	lists  int
}

// NewMemory returns an empty calendar source.
func NewMemory() *Memory { return &Memory{events: map[string]map[string]Event{}} }

// Put adds or replaces an event.
func (m *Memory) Put(calendar string, ev Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.events[calendar] == nil {
		m.events[calendar] = map[string]Event{}
	}
	m.events[calendar][ev.ID] = ev
}

// Delete removes an event, as Timely does when a booking is cancelled.
func (m *Memory) Delete(calendar, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.events[calendar], id)
}

// Fail makes every read return err until Fail(nil).
func (m *Memory) Fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

// Gets and Lists count calls, so tests can assert Calendar wasn't asked.
func (m *Memory) Gets() int  { m.mu.Lock(); defer m.mu.Unlock(); return m.gets }
func (m *Memory) Lists() int { m.mu.Lock(); defer m.mu.Unlock(); return m.lists }

// List returns the events overlapping [from, to). Timed events are treated
// as lasting an hour, enough for overlap tests.
func (m *Memory) List(_ context.Context, calendar string, from, to time.Time) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lists++
	if m.fail != nil {
		return nil, m.fail
	}
	var out []Event
	for _, ev := range m.events[calendar] {
		if ev.Cancelled || ev.Start.IsZero() {
			continue
		}
		if ev.Start.Before(to) && ev.Start.Add(time.Hour).After(from) {
			out = append(out, ev)
		}
	}
	return out, nil
}

// Get returns one event.
func (m *Memory) Get(_ context.Context, calendar, id string) (Event, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	if m.fail != nil {
		return Event{}, false, m.fail
	}
	ev, ok := m.events[calendar][id]
	return ev, ok, nil
}
