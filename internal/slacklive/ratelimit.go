package slacklive

import (
	"context"
	"errors"
	"time"

	"github.com/slack-go/slack"
)

// rateLimitRetryAfter reports whether err is a Slack rate-limit response and,
// if so, how long to wait before retrying.
func rateLimitRetryAfter(err error) (time.Duration, bool) {
	var rl *slack.RateLimitedError
	if !errors.As(err, &rl) {
		return 0, false
	}
	d := rl.RetryAfter
	if d <= 0 {
		d = time.Second
	}
	return d, true
}

// sleepForRetry waits for d or until ctx is cancelled, whichever comes first.
func sleepForRetry(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		d = time.Second
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
