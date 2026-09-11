package client

import "testing"

func TestParseGoogleCookiesInputJSON(t *testing.T) {
	cookies, err := ParseGoogleCookiesInput(`{"SID":"sid-value","SAPISID":"sap-value"}`)
	if err != nil {
		t.Fatalf("ParseGoogleCookiesInput(): %v", err)
	}
	if cookies["SID"] != "sid-value" || cookies["SAPISID"] != "sap-value" {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}
}

func TestParseGoogleCookiesInputCurl(t *testing.T) {
	curl := `curl 'https://messages.google.com/web/config' -H 'Cookie: SID=sid-value; SAPISID=sap-value'`
	cookies, err := ParseGoogleCookiesInput(curl)
	if err != nil {
		t.Fatalf("ParseGoogleCookiesInput(): %v", err)
	}
	if cookies["SID"] != "sid-value" || cookies["SAPISID"] != "sap-value" {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}
}

func TestParseGoogleCookiesInputHeader(t *testing.T) {
	cookies, err := ParseGoogleCookiesInput("Cookie: SID=sid-value; SAPISID=sap-value")
	if err != nil {
		t.Fatalf("ParseGoogleCookiesInput(): %v", err)
	}
	if cookies["SID"] != "sid-value" || cookies["SAPISID"] != "sap-value" {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}
}

func TestParseGoogleCookiesInputEmpty(t *testing.T) {
	if _, err := ParseGoogleCookiesInput("  "); err == nil {
		t.Fatal("expected error for empty input")
	}
}
