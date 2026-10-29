package logistics

import "time"

// RouteFitReconciliationConfig controls the fallback scan for shipments that
// stopped receiving webhook events. The RouteFit client is supplied by R-02;
// this package deliberately owns only scheduling policy.
type RouteFitReconciliationConfig struct {
	ScanEvery  time.Duration
	NoEventFor time.Duration
}

func (c RouteFitReconciliationConfig) normalized() RouteFitReconciliationConfig {
	if c.ScanEvery <= 0 {
		c.ScanEvery = 10 * time.Minute
	}
	if c.NoEventFor <= 0 {
		c.NoEventFor = 30 * time.Minute
	}
	return c
}

func (c RouteFitReconciliationConfig) Due(lastEventAt, now time.Time) bool {
	c = c.normalized()
	return !lastEventAt.IsZero() && !lastEventAt.After(now) && now.Sub(lastEventAt) >= c.NoEventFor
}
