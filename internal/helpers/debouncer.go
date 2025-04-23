package helpers

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// DebounceFunc defines the type for the function to be executed after debouncing.
type DebounceFunc func()

// Debouncer manages debouncing calls for different keys.
type Debouncer struct {
	mu     sync.Mutex
	timers map[string]*time.Timer // key: identifier for the debounced action
	delay  time.Duration
}

// NewDebouncer creates a new Debouncer.
func NewDebouncer(delay time.Duration) *Debouncer {
	return &Debouncer{
		timers: make(map[string]*time.Timer),
		delay:  delay,
	}
}

// Debounce schedules or resets the timer for a given key.
// When the delay expires without subsequent calls for the same key, the action function is executed.
func (d *Debouncer) Debounce(key string, action DebounceFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// If a timer already exists for this key, stop and reset it.
	if timer, ok := d.timers[key]; ok {
		timer.Stop()
		log.Trace().Str("key", key).Msg("Debouncer: Timer stopped")
	}

	// Create a new timer.
	d.timers[key] = time.AfterFunc(d.delay, func() {
		// This function runs after the delay has passed without new calls for this key.

		// Remove the timer entry *before* executing the action.
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()

		log.Trace().Str("key", key).Msg("Debouncer: Executing debounced action")
		// Execute the provided action function.
		action()
	})
	log.Trace().Str("key", key).Dur("delay", d.delay).Msg("Debouncer: Timer set/reset")
}

// Stop cancels all pending debounced actions.
func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	log.Debug().Int("pending_timers", len(d.timers)).Msg("Stopping debouncer and cancelling pending timers")
	for key, timer := range d.timers {
		timer.Stop()
		delete(d.timers, key)
	}
}
