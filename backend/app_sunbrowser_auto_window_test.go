package backend

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestSunBrowserWindowArgsWithBounds(t *testing.T) {
	bounds := sunBrowserWindowBounds{Left: 0, Top: 25, Width: 1920, Height: 986}
	prefix := []string{"--window-size=1920,986", "--window-position=0,25"}
	for _, tt := range []struct {
		name string
		args []string
		keep []string
	}{
		{"empty", nil, nil},
		{"inline", []string{"--window-size=800,600", "https://example.test", "--window-position=10,20"}, []string{"https://example.test"}},
		{"split", []string{"--window-size", "800,600", "--window-position", "-100, 20", "about:blank"}, []string{"about:blank"}},
		{"duplicates", []string{"--window-size=1,2", "--window-size", "3,4", "--window-position", "1,2", "--window-position=4,5"}, nil},
		{"other operands", []string{"--user-data-dir", "/tmp/a b", "--proxy-server=http://localhost:8080", "file:///tmp/index.html", "chrome://version", "https://example.test/?a=1,2"}, []string{"--user-data-dir", "/tmp/a b", "--proxy-server=http://localhost:8080", "file:///tmp/index.html", "chrome://version", "https://example.test/?a=1,2"}},
		{"missing operands", []string{"--window-size", "--window-position", "https://example.test", "--window-size"}, []string{"https://example.test"}},
		{"url first", []string{"https://example.test", "--new-window", "--window-size", "12,34", "about:blank"}, []string{"https://example.test", "--new-window", "about:blank"}},
		{"end of options", []string{"--window-size=3,4", "--", "--window-size=5,6", "https://example.test"}, []string{"--", "--window-size=5,6", "https://example.test"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := append([]string(nil), tt.args...)
			got, err := sunBrowserWindowArgsWithBounds(tt.args, bounds)
			if err != nil {
				t.Fatal(err)
			}
			want := append(append([]string(nil), prefix...), tt.keep...)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v; want %#v", got, want)
			}
			if !reflect.DeepEqual(tt.args, original) {
				t.Fatal("input arguments mutated")
			}
			again, err := sunBrowserWindowArgsWithBounds(got, bounds)
			if err != nil || !reflect.DeepEqual(got, again) {
				t.Fatalf("not idempotent: %#v, %v", again, err)
			}
		})
	}
}

func TestSunBrowserWindowInvalidBoundsPreserveArgs(t *testing.T) {
	args := []string{"https://example.test"}
	for _, b := range []sunBrowserWindowBounds{{}, {Width: -1, Height: 10}, {Width: 10, Height: 0}} {
		got, err := sunBrowserWindowArgsWithBounds(args, b)
		if err == nil || !reflect.DeepEqual(got, args) {
			t.Fatalf("got %#v, %v for %+v", got, err, b)
		}
	}
}

func TestSunBrowserWindowBoundsCache(t *testing.T) {
	var cache sunBrowserWindowBoundsCache
	now := time.Unix(100, 0)
	clock := func() time.Time { return now }
	calls := 0
	bounds := sunBrowserWindowBounds{Top: 25, Width: 1920, Height: 986}
	query := func() (sunBrowserWindowBounds, error) { calls++; return bounds, nil }
	for i := 0; i < 2; i++ {
		got, err := cache.get(clock, query)
		if err != nil || got != bounds {
			t.Fatalf("got %+v, %v", got, err)
		}
	}
	if calls != 1 {
		t.Fatalf("cache miss: %d calls", calls)
	}
	now = now.Add(30 * time.Second)
	if _, err := cache.get(clock, query); err != nil || calls != 2 {
		t.Fatalf("cache failed to expire: %d calls, %v", calls, err)
	}
}

func TestSunBrowserWindowBoundsCacheErrorsRetry(t *testing.T) {
	for _, tt := range []struct {
		name   string
		bounds sunBrowserWindowBounds
		err    error
	}{
		{"query error", sunBrowserWindowBounds{}, errors.New("probe failed")},
		{"invalid bounds", sunBrowserWindowBounds{Width: 0, Height: 10}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var cache sunBrowserWindowBoundsCache
			calls := 0
			query := func() (sunBrowserWindowBounds, error) { calls++; return tt.bounds, tt.err }
			for i := 0; i < 2; i++ {
				if _, err := cache.get(time.Now, query); err == nil {
					t.Fatal("expected probe/validation error")
				}
			}
			if calls != 2 {
				t.Fatalf("failure cached: %d calls", calls)
			}
		})
	}
}

func TestSunBrowserWindowBoundsCacheConcurrent(t *testing.T) {
	var cache sunBrowserWindowBoundsCache
	calls := 0 // Accessed only while cache.mu is held.
	query := func() (sunBrowserWindowBounds, error) {
		calls++
		return sunBrowserWindowBounds{Width: 1920, Height: 986}, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cache.get(time.Now, query); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("concurrent launches queried %d times", calls)
	}
}
