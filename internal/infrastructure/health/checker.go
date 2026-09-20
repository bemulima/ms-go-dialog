package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

type Check func(context.Context) error

type Checker struct {
	timeout time.Duration
	checks  map[string]Check
}

type checkResult struct {
	name string
	err  error
}

func NewChecker(timeout time.Duration, checks map[string]Check) *Checker {
	cloned := make(map[string]Check, len(checks))
	for name, check := range checks {
		if name != "" && check != nil {
			cloned[name] = check
		}
	}
	return &Checker{timeout: timeout, checks: cloned}
}

func (c *Checker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status, checks := c.Run(r.Context())
	code := http.StatusOK
	if status != "ready" {
		code = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}{Status: status, Checks: checks})
}

func (c *Checker) Run(parent context.Context) (string, map[string]string) {
	result := make(map[string]string, len(c.checks))
	if len(c.checks) == 0 {
		return "ready", result
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	names := make([]string, 0, len(c.checks))
	results := make(chan checkResult, len(c.checks))
	for name, check := range c.checks {
		names = append(names, name)
		go func(name string, check Check) {
			results <- checkResult{name: name, err: check(ctx)}
		}(name, check)
	}
	sort.Strings(names)
	pending := make(map[string]struct{}, len(names))
	for _, name := range names {
		pending[name] = struct{}{}
	}

	for len(pending) > 0 {
		select {
		case item := <-results:
			if _, exists := pending[item.name]; !exists {
				continue
			}
			if item.err == nil {
				result[item.name] = "ok"
			} else {
				result[item.name] = "failed"
			}
			delete(pending, item.name)
		case <-ctx.Done():
			for name := range pending {
				result[name] = "failed"
			}
			return "unavailable", result
		}
	}
	for _, state := range result {
		if state != "ok" {
			return "unavailable", result
		}
	}
	return "ready", result
}
