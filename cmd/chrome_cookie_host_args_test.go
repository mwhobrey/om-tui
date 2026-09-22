package cmd

import "testing"

func TestParseChromeCookieHostArgsIgnoresChromeLaunchSwitches(t *testing.T) {
	// Chrome on Windows appends --parent-window=<HWND> after the origin.
	// Before this was ignored, the host exited immediately and the extension
	// reported "native host not connected".
	opts, err := parseChromeCookieHostArgs([]string{
		"chrome-extension://akakhclmanbjmbojfbjcnakfbmobinee/",
		"--parent-window=0",
	})
	if err != nil {
		t.Fatalf("parseChromeCookieHostArgs(): %v", err)
	}
	if opts.install || opts.help {
		t.Fatalf("unexpected opts: %+v", opts)
	}

	opts, err = parseChromeCookieHostArgs([]string{
		"--parent-window", "1234",
		"chrome-extension://akakhclmanbjmbojfbjcnakfbmobinee/",
	})
	if err != nil {
		t.Fatalf("parseChromeCookieHostArgs(legacy order): %v", err)
	}
	if opts.install {
		t.Fatal("did not expect --install")
	}
}
