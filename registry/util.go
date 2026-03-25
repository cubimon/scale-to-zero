package registry

import (
	"context"
	"fmt"
	"time"
)

func retry(ctx context.Context, attempts int, sleep time.Duration, f func() error) error {
	for i := 0; i < attempts; i++ {
		if err := f(); err == nil {
			return nil
		}
		select {
		case <-time.After(sleep):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("timed out after %d attempts", attempts)
}
