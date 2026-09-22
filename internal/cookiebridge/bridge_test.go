package cookiebridge

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestValidateCookies(t *testing.T) {
	err := ValidateCookies(map[string]string{"SID": "a"})
	if err == nil {
		t.Fatal("expected missing cookies error")
	}
	cookies := map[string]string{
		"SID": "s", "HSID": "h", "SSID": "ss", "APISID": "a", "SAPISID": "sa", "OSID": "o",
	}
	if err := ValidateCookies(cookies); err != nil {
		t.Fatalf("ValidateCookies(): %v", err)
	}
}

func TestRegistryRequestRoundTrip(t *testing.T) {
	reg := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	waitDone := make(chan error, 1)
	go func() {
		req, err := reg.Wait(ctx, time.Second)
		if err != nil {
			waitDone <- err
			return
		}
		waitDone <- reg.Reply(req.ID, map[string]string{
			"SID": "s", "HSID": "h", "SSID": "ss", "APISID": "a", "SAPISID": "sa", "OSID": "o",
		}, nil)
	}()

	// Give the waiter a moment to register online.
	deadline := time.Now().Add(500 * time.Millisecond)
	for !reg.Online() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !reg.Online() {
		t.Fatal("expected bridge online while waiting")
	}

	cookies, err := reg.RequestCookies(ctx, time.Second)
	if err != nil {
		t.Fatalf("RequestCookies(): %v", err)
	}
	if cookies["SID"] != "s" {
		t.Fatalf("cookies = %#v", cookies)
	}
	if err := <-waitDone; err != nil {
		t.Fatalf("wait/reply: %v", err)
	}
}

func TestRegistryOffline(t *testing.T) {
	reg := New()
	_, err := reg.RequestCookies(context.Background(), 50*time.Millisecond)
	if !errors.Is(err, ErrBridgeOffline) {
		t.Fatalf("RequestCookies() = %v, want ErrBridgeOffline", err)
	}
}

func TestNativeMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteNativeMessage(&buf, map[string]any{"op": "hello", "n": 1}); err != nil {
		t.Fatalf("WriteNativeMessage(): %v", err)
	}
	var got map[string]any
	if err := ReadNativeMessage(&buf, &got); err != nil {
		t.Fatalf("ReadNativeMessage(): %v", err)
	}
	if got["op"] != "hello" {
		t.Fatalf("got = %#v", got)
	}
}
