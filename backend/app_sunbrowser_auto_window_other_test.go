//go:build !darwin

package backend

import (
	"reflect"
	"testing"
)

func TestSunBrowserAutoWindowNonDarwinNoop(t *testing.T) {
	for _, args := range [][]string{nil, {}, {"--window-size", "800,600", "https://example.test"}} {
		got, err := sunBrowserAutoWindowArgs(args)
		if err != nil || !reflect.DeepEqual(got, args) {
			t.Fatalf("got %#v, %v; want %#v", got, err, args)
		}
		if len(args) > 0 && &args[0] != &got[0] {
			t.Fatal("non-darwin no-op allocated arguments")
		}
	}
}
