//go:build !darwin

package backend

import "fmt"

// Other platforms retain their original launch arguments without allocation.
func sunBrowserAutoWindowArgs(args []string) ([]string, error) {
	return args, nil
}

func sunBrowserAutoWindowBounds() (sunBrowserWindowBounds, error) {
	return sunBrowserWindowBounds{}, fmt.Errorf("primary visible frame query is supported on macOS only")
}
