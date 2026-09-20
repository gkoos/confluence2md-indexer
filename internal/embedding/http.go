package embedding

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	retryBaseDelay = 500 * time.Millisecond
	retryMaxDelay  = 30 * time.Second
)

// httpStatusError reports a non-2xx embedding response.
type httpStatusError struct {
	StatusCode int
	Message    string
}

func (e *httpStatusError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("embedding endpoint returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("embedding endpoint returned status %d: %s", e.StatusCode, e.Message)
}

// retryable reports whether the request is worth attempting again.
func (e *httpStatusError) retryable() bool {
	switch e.StatusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// batchRequest performs one embedding request for a single batch of texts and
// returns the vectors in input order plus any delay the server asked for.
type batchRequest func(ctx context.Context, texts []string) ([][]float32, time.Duration, error)

// batchEmbedder splits inputs into provider-sized batches and retries transient
// failures with exponential backoff.
type batchEmbedder struct {
	batchSize  int
	maxRetries int
	request    batchRequest
	sleep      func(context.Context, time.Duration) error
}

func newBatchEmbedder(batchSize int, maxRetries int, request batchRequest) *batchEmbedder {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	if maxRetries == 0 {
		maxRetries = DefaultMaxRetries
	}
	return &batchEmbedder{
		batchSize:  batchSize,
		maxRetries: maxRetries,
		request:    request,
		sleep:      sleepContext,
	}
}

// embed processes every text, preserving input order across batches.
func (e *batchEmbedder) embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += e.batchSize {
		end := min(start+e.batchSize, len(texts))
		vectors, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		if len(vectors) != end-start {
			return nil, fmt.Errorf("embedding endpoint returned %d vectors for %d inputs", len(vectors), end-start)
		}
		out = append(out, vectors...)
	}
	return out, nil
}

func (e *batchEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	attempts := e.maxRetries + 1
	if e.maxRetries < 0 {
		attempts = 1
	}

	attemptsMade := 0
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		attemptsMade++

		vectors, retryAfter, err := e.request(ctx, texts)
		if err == nil {
			return vectors, nil
		}
		lastErr = err

		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if !retryableError(err) || attempt == attempts-1 {
			break
		}
		if sleepErr := e.sleep(ctx, backoffDelay(attempt, retryAfter)); sleepErr != nil {
			return nil, sleepErr
		}
	}

	// Report attempts actually made: a permanent failure such as a 401 is not
	// retried, and claiming the configured maximum would mislead the caller.
	return nil, fmt.Errorf("embedding request failed after %d attempt(s): %w", attemptsMade, lastErr)
}

// permanentError marks a failure that retrying cannot fix, such as a malformed
// response or invalid local configuration.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }

func (e *permanentError) Unwrap() error { return e.err }

// permanent wraps err so the retry loop stops immediately.
func permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// retryableError classifies failures. Permanent errors never retry, server-side
// status codes retry only when transient, and everything else is treated as a
// transport failure worth another attempt.
func retryableError(err error) bool {
	var permanentErr *permanentError
	if errors.As(err, &permanentErr) {
		return false
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return statusErr.retryable()
	}
	return true
}

// backoffDelay honours Retry-After when present and otherwise applies
// exponential backoff with full jitter, so parallel runs do not retry in lockstep.
func backoffDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, retryMaxDelay)
	}

	delay := retryBaseDelay << attempt
	if delay > retryMaxDelay {
		delay = retryMaxDelay
	}
	half := delay / 2
	if half <= 0 {
		return delay
	}
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// parseRetryAfter accepts both delay-seconds and HTTP-date forms.
func parseRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(header); err == nil {
		if delta := time.Until(when); delta > 0 {
			return delta
		}
	}
	return 0
}
