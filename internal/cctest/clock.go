package cctest

import "time"

type Clock struct{ At time.Time }

func (c Clock) Now() time.Time { return c.At }

func (Clock) After(d time.Duration) <-chan time.Time { return time.After(d) }
