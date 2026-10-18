// Package focus tracks user activity independently of daemon refreshes.
package focus

import "time"

type Clock struct {
	lastUser, minuteStart time.Time
	activeMinutes         int
	BufferUntil           time.Time
}

func New(now time.Time) Clock { return Clock{lastUser: now, minuteStart: now} }

func (c *Clock) Activity(now time.Time) { c.lastUser = now }

func (c *Clock) Expired(now time.Time) bool {
	if now.Sub(c.lastUser) >= 3*time.Minute {
		return true
	}
	if !c.BufferUntil.IsZero() {
		return !now.Before(c.BufferUntil)
	}
	for !now.Before(c.minuteStart.Add(time.Minute)) {
		if !c.lastUser.Before(c.minuteStart) {
			c.activeMinutes++
		} else {
			c.activeMinutes = 0
		}
		c.minuteStart = c.minuteStart.Add(time.Minute)
		if c.activeMinutes == 15 {
			c.BufferUntil = c.minuteStart.Add(30 * time.Second)
			return !now.Before(c.BufferUntil)
		}
	}
	return false
}
