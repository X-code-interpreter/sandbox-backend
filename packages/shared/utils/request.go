package utils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"syscall"
	"time"
)

// Retry http request when encounter EOF error
func RetryHttpRequest(ctx context.Context, httpReqFunc func() error, maxRetryTimes int) (int, error) {
	var err error
	retryTimes := 0
	retryInterval := 50 * time.Millisecond
	timer := time.NewTimer(retryInterval)
	// we will retry when encounter errors
	for {
		err = httpReqFunc()
		if err == nil {
			return retryTimes, nil
		}
		// we only retry with EOF error
		if e := (&url.Error{}); errors.As(err, &e) {
			switch {
			case errors.Is(e.Err, io.EOF):
				fallthrough
			case errors.Is(e.Err, syscall.ECONNREFUSED):
				goto cont
			}
		}
		return retryTimes, err
		// first check context
	cont:
		timer.Reset(retryInterval)
		select {
		case <-ctx.Done():
			return retryTimes, ctx.Err()
		case <-timer.C:
			if retryInterval < time.Second {
				retryInterval *= 2
			}
		}
		retryTimes += 1
		if retryTimes > maxRetryTimes {
			return retryTimes, fmt.Errorf("reach max retry times, last error: %w", err)
		}
	}
}
