package github

import (
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// Retryable reports a failed request that a later try may well not meet, as when GitHub
// has a bad minute: GitHub answering with a server error (5xx), refusing it for its rate
// limits or asking the app to wait (RateLimited), or a GraphQL query refused for them, and
// a request GitHub never answered in full (no connection, a timeout, an answer cut off). A
// refused token, a missing scope, a request GitHub turned down, and anything that isn't
// GitHub's are not: trying them again changes nothing until the user does something.
func Retryable(err error) bool {
	if err == nil || IsBadCredentials(err) {
		return false
	}

	if status, ok := errors.AsType[*StatusError](err); ok {
		return status.StatusCode >= http.StatusInternalServerError || RateLimited(err)
	}

	if failed, ok := errors.AsType[*queryError](err); ok {
		return failed.limited
	}

	// A request that got no answer, or only part of one: every failure of the HTTP client
	// is a *url.Error, which is a net.Error, and so is a connection that broke while the
	// answer was read.
	if _, ok := errors.AsType[net.Error](err); ok {
		return true
	}

	return errors.Is(err, io.ErrUnexpectedEOF)
}

// RetryAfter is how long GitHub asked the app to wait before it tries again after err:
// what its answer's Retry-After said, or a minute for a rate limit that said nothing; 0
// for any other error.
func RetryAfter(err error) time.Duration {
	return writeHold(err)
}
