package whatsapplive

import (
	"context"

	wastore "go.mau.fi/whatsmeow/store"
	watypes "go.mau.fi/whatsmeow/types"
)

const (
	senderTestGroup = "whatsapp:120363019999999999@g.us"
	senderTestPhone = "15557654321"
	senderTestLID   = "98765432109876"
)

// senderTestLIDs is the account's LID map in reaction and read-receipt
// tests: the phone number each known LID stands for, by user part.
type senderTestLIDs map[string]string

func (m senderTestLIDs) PutManyLIDMappings(context.Context, []wastore.LIDMapping) error {
	return nil
}

func (m senderTestLIDs) PutLIDMapping(context.Context, watypes.JID, watypes.JID) error {
	return nil
}

func (m senderTestLIDs) GetPNForLID(_ context.Context, lid watypes.JID) (watypes.JID, error) {
	if pn, ok := m[lid.User]; ok {
		return watypes.NewJID(pn, watypes.DefaultUserServer), nil
	}
	return watypes.EmptyJID, nil
}

func (m senderTestLIDs) GetLIDForPN(_ context.Context, pn watypes.JID) (watypes.JID, error) {
	for lid, number := range m {
		if number == pn.User {
			return watypes.NewJID(lid, watypes.HiddenUserServer), nil
		}
	}
	return watypes.EmptyJID, nil
}

func (m senderTestLIDs) GetManyLIDsForPNs(ctx context.Context, pns []watypes.JID) (map[watypes.JID]watypes.JID, error) {
	result := make(map[watypes.JID]watypes.JID)
	for _, pn := range pns {
		if lid, _ := m.GetLIDForPN(ctx, pn); !lid.IsEmpty() {
			result[pn] = lid
		}
	}
	return result, nil
}

// senderTestBridge is a connected test bridge whose account knows the LIDs
// in lids.
func senderTestBridge(lids senderTestLIDs) *Bridge {
	bridge := connectedMediaTestBridge()
	bridge.client.Store.LIDs = lids
	return bridge
}
