//go:build darwin

package backend

import (
	"os"
	"testing"
)

func TestSunBrowserDecodeWindowBounds(t *testing.T) {
	got, err := sunBrowserDecodeWindowBounds([]byte(`{"left":0,"top":25,"width":1920,"height":986}`))
	want := sunBrowserWindowBounds{Top: 25, Width: 1920, Height: 986}
	if err != nil || got != want {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
	for _, input := range []string{`{`, `{}`, `null`, `{"width":0,"height":986}`, `{"width":1920,"height":-1}`, `{"width":1.5,"height":2}`} {
		if _, err := sunBrowserDecodeWindowBounds([]byte(input)); err == nil {
			t.Fatalf("accepted invalid bounds %s", input)
		}
	}
}

func TestSunBrowserQueryVisibleFrameFallback(t *testing.T) {
	want := sunBrowserWindowBounds{Left: 0, Top: 30, Width: 1920, Height: 980}
	calledFallback := false
	got, err := sunBrowserQueryVisibleFrameWithProbes(
		func() (sunBrowserWindowBounds, error) { return sunBrowserWindowBounds{}, os.ErrNotExist },
		func() (sunBrowserWindowBounds, error) { calledFallback = true; return want, nil },
	)
	if err != nil || !calledFallback || got != want {
		t.Fatalf("fallback failed: got=%+v err=%v called=%v", got, err, calledFallback)
	}

	calledFallback = false
	got, err = sunBrowserQueryVisibleFrameWithProbes(
		func() (sunBrowserWindowBounds, error) { return want, nil },
		func() (sunBrowserWindowBounds, error) { calledFallback = true; return sunBrowserWindowBounds{}, os.ErrInvalid },
	)
	if err != nil || calledFallback || got != want {
		t.Fatalf("primary probe should short-circuit fallback: got=%+v err=%v called=%v", got, err, calledFallback)
	}
}

// Opt-in diagnostic: no window is launched, no settings changed, and no UI
// scripting/screenshot permission is used. The probe runs AppKit directly.
// SUNBROWSER_INSPECT_BOUNDS=1 go test ./backend -run '^TestSunBrowserActualVisibleFrame$' -v -count=1
func TestSunBrowserActualVisibleFrame(t *testing.T) {
	if os.Getenv("SUNBROWSER_INSPECT_BOUNDS") != "1" {
		t.Skip("set SUNBROWSER_INSPECT_BOUNDS=1 to query actual AppKit visibleFrame")
	}
	bounds, err := sunBrowserAutoWindowBounds()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("primary visibleFrame (logical points, CDP coordinates): %+v", bounds)
	args, err := sunBrowserAutoWindowArgs([]string{"--window-size", "800,600", "about:blank"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("startup arguments: %q", args)
}
