// Package focus tracks user activity independently of daemon refreshes.
package focus

import "time"

type Clock struct {
	lastUser time.Time
}

func New(now time.Time) Clock { return Clock{lastUser: now} }

func (c *Clock) Activity(now time.Time) { c.lastUser = now }

func (c *Clock) Expired(now time.Time) bool {
	return now.Sub(c.lastUser) >= 3*time.Minute
}
