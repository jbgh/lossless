package s3

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

var ErrNotFound = errors.New("s3: not found")

// StatusError is a non-2xx answer the client did not retry (or gave up on).
type StatusError struct {
	Op     string
	Key    string
	Status int
	Code   string
}

func (e *StatusError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("s3 %s %s: %d %s", e.Op, e.Key, e.Status, e.Code)
	}
	return fmt.Sprintf("s3 %s %s: %d", e.Op, e.Key, e.Status)
}

type Client struct {
	cfg   Config
	http  *http.Client
	now   func() time.Time
	sleep func(time.Duration)
}

var backoff = []time.Duration{time.Second, 4 * time.Second, 16 * time.Second}

func New(cfg Config) (*Client, error) {
	n, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	return &Client{
		cfg: n,
		http: &http.Client{
			Timeout: 10 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return fmt.Errorf("redirects disabled")
			},
		},
		now:   time.Now,
		sleep: time.Sleep,
	}, nil
}

// SetSleep replaces the backoff sleeper. Tests pass a no-op.
func (c *Client) SetSleep(f func(time.Duration)) { c.sleep = f }

// Put uploads body (size bytes, hex sha256hex) under key. body must seek so
// a retry can rewind it.
func (c *Client) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, sha256hex string) error {
	res, err := c.do(ctx, "PUT", key, func() (*http.Request, error) {
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		u, err := c.cfg.objectURL(key)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), io.NopCloser(body))
		if err != nil {
			return nil, err
		}
		req.ContentLength = size
		sign(req, c.cfg, sha256hex, c.now())
		return req, nil
	})
	if err != nil {
		return err
	}
	drain(res)
	return nil
}

// Get returns the body and its length. The caller closes the body.
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	res, err := c.do(ctx, "GET", key, func() (*http.Request, error) {
		return c.bodyless(ctx, http.MethodGet, key)
	})
	if err != nil {
		return nil, 0, err
	}
	return res.Body, res.ContentLength, nil
}

func (c *Client) Head(ctx context.Context, key string) (int64, error) {
	res, err := c.do(ctx, "HEAD", key, func() (*http.Request, error) {
		return c.bodyless(ctx, http.MethodHead, key)
	})
	if err != nil {
		return 0, err
	}
	drain(res)
	if cl := res.Header.Get("Content-Length"); cl != "" {
		n, _ := strconv.ParseInt(cl, 10, 64)
		return n, nil
	}
	return res.ContentLength, nil
}

// Delete removes key. A missing key is success.
func (c *Client) Delete(ctx context.Context, key string) error {
	res, err := c.do(ctx, "DELETE", key, func() (*http.Request, error) {
		return c.bodyless(ctx, http.MethodDelete, key)
	})
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	drain(res)
	return nil
}

func (c *Client) bodyless(ctx context.Context, method, key string) (*http.Request, error) {
	u, err := c.cfg.objectURL(key)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	sign(req, c.cfg, emptyPayloadHash, c.now())
	return req, nil
}

// do runs build() and sends the request, retrying on transport errors,
// 429, and 5xx with the fixed backoff. Any other non-2xx returns at once.
// A 404 returns ErrNotFound. A 2xx response is returned with its body open.
func (c *Client) do(ctx context.Context, op, key string, build func() (*http.Request, error)) (*http.Response, error) {
	var last error
	for attempt := 0; attempt <= len(backoff); attempt++ {
		if attempt > 0 {
			c.sleep(backoff[attempt-1])
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		req, err := build()
		if err != nil {
			return nil, err
		}
		res, err := c.http.Do(req)
		if err != nil {
			last = fmt.Errorf("s3 %s %s: %w", op, key, err)
			continue
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return res, nil
		}
		code := readErrorCode(res)
		se := &StatusError{Op: op, Key: key, Status: res.StatusCode, Code: code}
		if res.StatusCode == 404 {
			return nil, fmt.Errorf("%w (%s)", ErrNotFound, se.Error())
		}
		if res.StatusCode == 429 || res.StatusCode >= 500 {
			last = se
			continue
		}
		return nil, se
	}
	return nil, last
}

func readErrorCode(res *http.Response) string {
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var e struct {
		Code string `xml:"Code"`
	}
	if xml.Unmarshal(b, &e) == nil {
		return e.Code
	}
	return ""
}

func drain(res *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	_ = res.Body.Close()
}
