package sqlite

import (
	"testing"
	"time"
)

func TestAddressBookContactRoundTrip(t *testing.T) {
	store, _ := openMessageTestRepository(t, func() time.Time { return time.UnixMilli(messageTestTimeMS) })

	if err := store.UpsertAddressBookContact("(615) 555-0100", "Alice"); err != nil {
		t.Fatalf("UpsertAddressBookContact: %v", err)
	}
	if err := store.UpsertAddressBookContact("(615) 555-0100", "Alice Example"); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	listed, err := store.ListAddressBookContacts("alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed = %+v, want 1", listed)
	}
	if listed[0].Name != "Alice Example" || listed[0].Number != "+16155550100" {
		t.Fatalf("contact = %+v", listed[0])
	}

	miss, err := store.ListAddressBookContacts("nobody", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(miss) != 0 {
		t.Fatalf("miss = %+v, want empty", miss)
	}
}
