package backend

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sunBrowserWindowBounds is the primary display's usable rectangle in logical
// points, in Chromium/CDP top-left coordinates (not AppKit bottom-left ones).
// It can also be passed as the bounds argument of Browser.setWindowBounds when
// restoring a saved window; launch switches alone do not override saved bounds.
// These are actual window bounds, never emulated screen/hardware properties.
type sunBrowserWindowBounds struct {
	Left   int `json:"left"`
	Top    int `json:"top"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (b sunBrowserWindowBounds) validate() error {
	if b.Width <= 0 || b.Height <= 0 {
		return fmt.Errorf("invalid primary display visible frame: %+v", b)
	}
	return nil
}

// sunBrowserWindowArgsWithBounds does not mutate args. Prepending the replacement
// flags keeps them before every URL and before a possible end-of-options marker,
// without guessing which operands belong to unrelated Chromium switches.
func sunBrowserWindowArgsWithBounds(args []string, b sunBrowserWindowBounds) ([]string, error) {
	if err := b.validate(); err != nil {
		return args, err
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, fmt.Sprintf("--window-size=%d,%d", b.Width, b.Height),
		fmt.Sprintf("--window-position=%d,%d", b.Left, b.Top))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			// Everything after -- is a target, even if it resembles a switch.
			out = append(out, args[i:]...)
			break
		}
		if strings.HasPrefix(arg, "--window-size=") || strings.HasPrefix(arg, "--window-position=") {
			continue
		}
		if arg == "--window-size" || arg == "--window-position" {
			// Consume split numeric operands, including negative positions. Never
			// consume another switch or URL when a flag is missing its operand.
			if i+1 < len(args) && sunBrowserWindowPair(args[i+1]) {
				i++
			}
			continue
		}
		out = append(out, arg)
	}
	return out, nil
}

func sunBrowserWindowPair(value string) bool {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if _, err := strconv.Atoi(strings.TrimSpace(part)); err != nil {
			return false
		}
	}
	return true
}

// Serialize cache misses as well as hits: concurrent launches start only one
// AppKit query. Failed probes are not cached, so later launches can retry.
type sunBrowserWindowBoundsCache struct {
	mu      sync.Mutex
	bounds  sunBrowserWindowBounds
	expires time.Time
}

func (c *sunBrowserWindowBoundsCache) get(now func() time.Time, query func() (sunBrowserWindowBounds, error)) (sunBrowserWindowBounds, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now().Before(c.expires) {
		return c.bounds, nil
	}
	bounds, err := query()
	if err == nil {
		err = bounds.validate()
	}
	if err != nil {
		return sunBrowserWindowBounds{}, err
	}
	c.bounds = bounds
	c.expires = now().Add(30 * time.Second)
	return bounds, nil
}
