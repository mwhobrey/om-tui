package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/v2keys"
)

const googleAddressBookAccountID = "google-primary"

// UpsertAddressBookContact writes a Google address-book row as an e164 identity
// linked to a person with address_book provenance. Empty phone is skipped.
func (s *Store) UpsertAddressBookContact(phone, name string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("upsert address book contact: store is nil")
	}
	canonical := canonicalAddressBookPhone(phone)
	if canonical == "" {
		return fmt.Errorf("upsert address book contact: phone is empty")
	}
	name = strings.TrimSpace(name)
	key, err := v2keys.IdentityKey(googleAddressBookAccountID, "sms", canonical)
	if err != nil {
		return fmt.Errorf("upsert address book contact: %w", err)
	}
	nowMS := time.Now().UnixMilli()
	if err := s.ensureGoogleAddressBookAccount(nowMS); err != nil {
		return err
	}

	identity := Identity{
		IdentityID:     v2keys.DeriveID("identity", googleAddressBookAccountID, key.Kind+"\x1f"+key.Canonical),
		AccountID:      googleAddressBookAccountID,
		Kind:           IdentityKind(key.Kind),
		CanonicalValue: key.Canonical,
		RawValue:       strings.TrimSpace(phone),
		DisplayName:    name,
		MetadataJSON:   "{}",
		CreatedAtMS:    nowMS,
		UpdatedAtMS:    nowMS,
	}
	if existing, err := s.GetIdentityByCanonical(googleAddressBookAccountID, identity.Kind, identity.CanonicalValue); err == nil {
		identity.IdentityID = existing.IdentityID
		identity.CreatedAtMS = existing.CreatedAtMS
		if identity.DisplayName == "" {
			identity.DisplayName = existing.DisplayName
		}
		identity.IsSelf = existing.IsSelf
		if strings.TrimSpace(existing.MetadataJSON) != "" {
			identity.MetadataJSON = existing.MetadataJSON
		}
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err := s.UpsertIdentity(identity); err != nil {
		return err
	}
	stored, err := s.GetIdentityByCanonical(googleAddressBookAccountID, identity.Kind, identity.CanonicalValue)
	if err != nil {
		return err
	}

	personID := v2keys.DeriveID("person", googleAddressBookAccountID, key.Kind+"\x1f"+key.Canonical)
	if link, err := s.GetPersonIdentity(stored.IdentityID); err == nil {
		personID = link.PersonID
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	} else {
		if err := s.CreatePerson(Person{
			PersonID:    personID,
			DisplayName: name,
			SortName:    strings.ToLower(name),
			CreatedAtMS: nowMS,
			UpdatedAtMS: nowMS,
		}); err != nil {
			if _, getErr := s.GetPerson(personID); getErr != nil {
				return err
			}
		}
		if err := s.LinkIdentityToPerson(PersonIdentity{
			IdentityID: stored.IdentityID,
			PersonID:   personID,
			Provenance: IdentityProvenanceAddressBook,
			Confidence: 1,
			IsPrimary:  true,
			LinkedAtMS: nowMS,
		}); err != nil {
			if _, getErr := s.GetPersonIdentity(stored.IdentityID); getErr != nil {
				return err
			}
		}
	}
	if name == "" {
		return nil
	}
	_, err = s.db.ExecContext(context.Background(), `
		UPDATE people
		SET display_name = ?,
		    sort_name = ?,
		    updated_at_ms = max(updated_at_ms, created_at_ms, ?)
		WHERE person_id = ?
	`, name, strings.ToLower(name), nowMS, personID)
	if err != nil {
		return fmt.Errorf("update person %q display name: %w", personID, err)
	}
	return nil
}

// ListAddressBookContacts returns Google e164 identities for autocomplete.
func (s *Store) ListAddressBookContacts(query string, limit int) ([]*db.Contact, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("list address book contacts: store is nil")
	}
	if limit <= 0 {
		limit = 50
	}
	queryLower := strings.ToLower(strings.TrimSpace(query))
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT i.identity_id,
		       CASE WHEN COALESCE(p.display_name, '') != '' THEN p.display_name ELSE i.display_name END,
		       i.canonical_value
		FROM identities AS i
		LEFT JOIN person_identities AS pi ON pi.identity_id = i.identity_id
		LEFT JOIN people AS p ON p.person_id = pi.person_id
		WHERE i.account_id = ?
		  AND i.kind = 'e164'
		  AND i.is_self = 0
		  AND (
		    ? = ''
		    OR instr(lower(i.display_name), ?) > 0
		    OR instr(lower(i.canonical_value), ?) > 0
		    OR instr(lower(COALESCE(p.display_name, '')), ?) > 0
		  )
		ORDER BY lower(CASE WHEN COALESCE(p.display_name, '') != '' THEN p.display_name ELSE i.display_name END),
		         i.canonical_value
		LIMIT ?
	`, googleAddressBookAccountID, queryLower, queryLower, queryLower, queryLower, limit)
	if err != nil {
		return nil, fmt.Errorf("list address book contacts: %w", err)
	}
	defer rows.Close()

	var contacts []*db.Contact
	for rows.Next() {
		c := &db.Contact{}
		if err := rows.Scan(&c.ContactID, &c.Name, &c.Number); err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

func (s *Store) ensureGoogleAddressBookAccount(nowMS int64) error {
	if _, err := s.GetAccount(googleAddressBookAccountID); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.UpsertAccount(Account{
		AccountID:   googleAddressBookAccountID,
		BridgeKey:   "google_messages",
		DisplayName: "Google Messages",
		Mode:        AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: nowMS,
		UpdatedAtMS: nowMS,
	})
}

func canonicalAddressBookPhone(raw string) string {
	normalized := db.NormalizeAvatarPhone(raw)
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return ""
	}
	if strings.HasPrefix(normalized, "+") {
		return normalized
	}
	return "+" + normalized
}
