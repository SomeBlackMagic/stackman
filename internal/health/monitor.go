package health

import (
	"context"
	"time"
)

// Monitor defines health waiting behavior for a service update.
type Monitor interface {
	WaitHealthy(ctx context.Context, timeout time.Duration) error
}
