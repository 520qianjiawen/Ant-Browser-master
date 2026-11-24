//go:build darwin

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

var sunBrowserDisplayBoundsCache sunBrowserWindowBoundsCache

func sunBrowserAutoWindowArgs(args []string) ([]string, error) {
	bounds, err := sunBrowserAutoWindowBounds()
	if err != nil {
		return args, err
	}
	return sunBrowserWindowArgsWithBounds(args, bounds)
}

// sunBrowserAutoWindowBounds exposes the same supported rectangle for CDP
// Browser.setWindowBounds after startup/session restore. It does not change
// resolution, dock preferences, screen emulation, or global display settings.
func sunBrowserAutoWindowBounds() (sunBrowserWindowBounds, error) {
	return sunBrowserDisplayBoundsCache.get(time.Now, sunBrowserQueryVisibleFrame)
}

// NSScreen must be queried on the process main thread. A cgo caller may be on
// a Go worker OR the Wails main thread; dispatch_sync(main) can deadlock the
// latter, while blindly querying AppKit on the former is unsupported. Running
// a short subprocess gives AppKit its own main thread and autorelease pool,
// independent of the parent's event loop, and also works with CGO_ENABLED=0.
// Prefer macOS built-in osascript (JXA) so Macs without Xcode Command Line Tools
// work out of the box, and fall back to /usr/bin/swift if needed.
// Successful results are cached for 30 seconds across browser launches.
func sunBrowserQueryVisibleFrame() (sunBrowserWindowBounds, error) {
	return sunBrowserQueryVisibleFrameWithProbes(
		sunBrowserQueryVisibleFrameJXA,
		sunBrowserQueryVisibleFrameSwift,
	)
}

func sunBrowserQueryVisibleFrameWithProbes(probes ...func() (sunBrowserWindowBounds, error)) (sunBrowserWindowBounds, error) {
	var lastErr error
	for _, probe := range probes {
		if probe == nil {
			continue
		}
		bounds, err := probe()
		if err == nil {
			return bounds, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		return sunBrowserWindowBounds{}, fmt.Errorf("no macOS visible frame probe configured")
	}
	return sunBrowserWindowBounds{}, lastErr
}

func sunBrowserQueryVisibleFrameJXA() (sunBrowserWindowBounds, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", sunBrowserVisibleFrameJXA)
	cmd.WaitDelay = time.Second
	data, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return sunBrowserWindowBounds{}, fmt.Errorf("query macOS primary visible frame using osascript: %w", err)
	}
	return sunBrowserDecodeWindowBounds(data)
}

func sunBrowserQueryVisibleFrameSwift() (sunBrowserWindowBounds, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/swift", "-e", sunBrowserVisibleFrameSwift)
	cmd.WaitDelay = time.Second
	data, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return sunBrowserWindowBounds{}, fmt.Errorf("query macOS primary visible frame using AppKit: %w", err)
	}
	return sunBrowserDecodeWindowBounds(data)
}

func sunBrowserDecodeWindowBounds(data []byte) (sunBrowserWindowBounds, error) {
	var bounds sunBrowserWindowBounds
	if err := json.Unmarshal(data, &bounds); err != nil {
		return bounds, fmt.Errorf("decode macOS primary visible frame: %w", err)
	}
	return bounds, bounds.validate()
}

// Built-in macOS JXA probe: requires no Xcode Command Line Tools.
const sunBrowserVisibleFrameJXA = `
ObjC.import("AppKit");
ObjC.import("CoreGraphics");
const primaryID = $.CGMainDisplayID();
const screens = $.NSScreen.screens;
let target = null;
for (let i = 0; i < screens.count; i++) {
  const s = screens.objectAtIndex(i);
  const num = s.deviceDescription.objectForKey($("NSScreenNumber"));
  if (num && ObjC.unwrap(num) === primaryID) {
    target = s;
    break;
  }
}
if (!target) {
  throw new Error("primary NSScreen unavailable");
}
const frame = target.frame;
const visible = target.visibleFrame;
const frameMaxY = frame.origin.y + frame.size.height;
const left = Math.ceil(visible.origin.x);
const top = Math.ceil(frameMaxY - (visible.origin.y + visible.size.height));
const right = Math.floor(visible.origin.x + visible.size.width);
const bottom = Math.floor(frameMaxY - visible.origin.y);
JSON.stringify({ left: left, top: top, width: right - left, height: bottom - top });
`

// NSScreen.main follows the key window and may be a secondary monitor. Match
// NSScreenNumber to CGMainDisplayID instead. AppKit coordinates are in logical
// points; convert the usable rectangle to top-left screen coordinates, rounding
// inward so fractional edges cannot put a window beneath the dock or menu.
const sunBrowserVisibleFrameSwift = `
import AppKit
import CoreGraphics
import Foundation

autoreleasepool {
    let primaryID = CGMainDisplayID()
    guard let screen = NSScreen.screens.first(where: {
        ($0.deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber)?.uint32Value == primaryID
    }) else {
        fputs("primary NSScreen unavailable\n", stderr)
        exit(1)
    }
    let visible = screen.visibleFrame
    let left = ceil(visible.minX)
    let top = ceil(screen.frame.maxY - visible.maxY)
    let right = floor(visible.maxX)
    let bottom = floor(screen.frame.maxY - visible.minY)
    let bounds: [String: Int] = [
        "left": Int(left), "top": Int(top),
        "width": Int(right - left), "height": Int(bottom - top)
    ]
    guard bounds["width"]! > 0, bounds["height"]! > 0,
          let data = try? JSONSerialization.data(withJSONObject: bounds, options: [.sortedKeys]),
          let json = String(data: data, encoding: .utf8) else {
        fputs("invalid primary visible frame\n", stderr)
        exit(2)
    }
    print(json)
}
`
