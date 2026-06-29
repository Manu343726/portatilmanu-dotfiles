package main

import (
	"sync"
	"time"
)

// SmartPoller implements adaptive polling: it polls more frequently when the
// plugin is actively being used, and stops polling entirely when idle. RPC
// handlers call NoteCall() to register activity, and PollNow() to request a
// synchronous poll on cold start so the caller doesn't receive stale data.
type SmartPoller struct {
	mu       sync.Mutex
	calls    []time.Time // sliding window of call timestamps
	lastPoll time.Time   // zero means never polled

	// pollReqCh coordinates wake/trigger requests between RPC handlers and
	// the Run loop:
	//   - NoteCall sends nil         → wake from idle (non-blocking)
	//   - PollNow sends done channel → trigger sync poll (blocking until done)
	pollReqCh chan chan struct{}

	idleTimeout time.Duration
	minInterval time.Duration
	maxInterval time.Duration
	decayWindow time.Duration
}

// NewSmartPoller creates a SmartPoller with sensible defaults:
//   - idle after 30s with no calls
//   - poll every 2s under heavy use (10+ calls/min)
//   - poll every 10s under light use (1 call/min)
//   - call frequency decays over a 60s window
func NewSmartPoller() *SmartPoller {
	return &SmartPoller{
		calls:       make([]time.Time, 0, 100),
		pollReqCh:   make(chan chan struct{}, 4),
		idleTimeout: 30 * time.Second,
		minInterval: 2 * time.Second,
		maxInterval: 10 * time.Second,
		decayWindow: 60 * time.Second,
	}
}

// NoteCall records an incoming RPC call and wakes the poller if it was idle.
// Safe to call concurrently; non-blocking when the poller is already active.
func (p *SmartPoller) NoteCall() {
	p.mu.Lock()
	now := time.Now()
	p.calls = append(p.calls, now)
	p.trimCallsLocked(now)
	isIdle := p.isIdleLocked(now)
	p.mu.Unlock()

	if isIdle {
		select {
		case p.pollReqCh <- nil:
		default:
		}
	}
}

// PollNow triggers a synchronous poll and blocks until fresh data is
// available. Used by RPC handlers on cold start so callers never see stale
// or zero-valued data. If a poll is already in progress, PollNow waits for
// that poll to complete rather than issuing a duplicate.
func (p *SmartPoller) PollNow() {
	done := make(chan struct{})
	select {
	case p.pollReqCh <- done:
		<-done
	default:
		// A poll request is already queued or in flight; data will be
		// fresh shortly — no need to enqueue another.
	}
}

// IsStale returns true when the plugin has been idle long enough that the
// cached data is likely out of date. RPC handlers check this after NoteCall
// to decide whether to call PollNow.
func (p *SmartPoller) IsStale() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isIdleLocked(time.Now())
}

func (p *SmartPoller) isIdleLocked(now time.Time) bool {
	return p.lastPoll.IsZero() || now.Sub(p.lastPoll) > p.idleTimeout
}

// Run starts the adaptive poll loop and blocks until stop is closed.
// An initial poll is performed synchronously on entry so data is available
// immediately. The pollFn is called with no arguments — it should capture
// state via closure.
func (p *SmartPoller) Run(stop <-chan struct{}, pollFn func()) {
	// Initial poll so data is ready before any RPC handler runs.
	pollFn()
	p.recordPoll()

	for {
		interval := p.nextInterval()

		if interval < 0 {
			// Idle — wait for a wake-up or stop signal.
			select {
			case <-stop:
				return
			case req := <-p.pollReqCh:
				pollFn()
				p.recordPoll()
				if req != nil {
					close(req)
				}
			}
		} else {
			select {
			case <-stop:
				return
			case <-time.After(interval):
				pollFn()
				p.recordPoll()
			case req := <-p.pollReqCh:
				pollFn()
				p.recordPoll()
				if req != nil {
					close(req)
				}
			}
		}
	}
}

// nextInterval calculates how long to wait before the next poll based on
// recent call frequency. Returns -1 when the poller should go idle.
func (p *SmartPoller) nextInterval() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	p.trimCallsLocked(now)
	recent := p.countRecentCallsLocked(now)

	// Go idle when there have been no calls for longer than idleTimeout.
	if recent == 0 && !p.lastPoll.IsZero() && now.Sub(p.lastPoll) > p.idleTimeout {
		return -1
	}

	return p.calcIntervalLocked(recent)
}

// calcIntervalLocked maps recent call count to a poll interval.
//
//	0  calls → maxInterval (10s) — barely active, keep ticking slowly
//	1  call  → ~9.2s
//	5  calls → ~6s
//	10+ calls → minInterval (2s) — actively used, poll fast
func (p *SmartPoller) calcIntervalLocked(recentCalls int) time.Duration {
	if recentCalls <= 0 {
		return p.maxInterval
	}

	const busyThreshold = 10
	if recentCalls >= busyThreshold {
		return p.minInterval
	}

	ratio := float64(recentCalls) / float64(busyThreshold)
	interval := float64(p.maxInterval) - ratio*(float64(p.maxInterval)-float64(p.minInterval))
	return time.Duration(interval)
}

func (p *SmartPoller) countRecentCallsLocked(now time.Time) int {
	cutoff := now.Add(-p.decayWindow)
	n := 0
	for _, t := range p.calls {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

func (p *SmartPoller) trimCallsLocked(now time.Time) {
	cutoff := now.Add(-p.decayWindow)
	i := 0
	for i < len(p.calls) && !p.calls[i].After(cutoff) {
		i++
	}
	if i > 0 {
		p.calls = p.calls[i:]
	}
}

func (p *SmartPoller) recordPoll() {
	p.mu.Lock()
	p.lastPoll = time.Now()
	p.mu.Unlock()
}
