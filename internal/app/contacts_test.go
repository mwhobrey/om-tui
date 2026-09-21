package app

import (
	"testing"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

type fakeAddressBook struct {
	phones []string
	names  []string
}

func (f *fakeAddressBook) UpsertAddressBookContact(phone, name string) error {
	f.phones = append(f.phones, phone)
	f.names = append(f.names, name)
	return nil
}

func TestSyncGoogleContactsWritesAddressBook(t *testing.T) {
	mock := &mockGMClient{
		contacts: []*gmproto.Contact{{
			ContactID: "contact-1",
			Name:      "Alice",
			Number:    &gmproto.ContactNumber{Number: "(615) 555-0100"},
		}},
	}
	a := newTestApp(t, mock)
	book := &fakeAddressBook{}
	a.SetAddressBook(book)

	count, err := a.SyncGoogleContacts()
	if err != nil {
		t.Fatalf("SyncGoogleContacts() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if len(book.phones) != 1 || book.phones[0] != "(615) 555-0100" || book.names[0] != "Alice" {
		t.Fatalf("address book = phones %v names %v", book.phones, book.names)
	}
	listed, err := a.Store.ListContacts("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("v1 contacts = %+v, want empty on PRIMARY address-book path", listed)
	}
}
