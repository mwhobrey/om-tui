package river

import "strings"

// ScopedID namespaces a remote conversation/message id to a river. The built-in
// *-default rivers keep the historical `whatsapp:<jid>` / `signal:+e164` form
// so existing threads do not move. Extra rivers use `whatsapp/<riverID>/<jid>`
// so two accounts chatting with the same peer cannot collide on PK.
func ScopedID(provider, riverID, remote string) string {
	provider = NormalizeProvider(provider)
	remote = strings.TrimSpace(remote)
	if provider == "" || remote == "" {
		return remote
	}
	if IsDefaultRiverID(riverID) || strings.TrimSpace(riverID) == "" {
		return provider + ":" + remote
	}
	return provider + "/" + strings.TrimSpace(riverID) + "/" + remote
}

// ScopedGroupID is ScopedID for Signal groups (`signal-group:` vs `signal-group/<river>/`).
func ScopedGroupID(riverID, groupID string) string {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return ""
	}
	if IsDefaultRiverID(riverID) || strings.TrimSpace(riverID) == "" {
		return "signal-group:" + groupID
	}
	return "signal-group/" + strings.TrimSpace(riverID) + "/" + groupID
}

// UnscopeID strips a river namespace and returns the remote id the live
// transport understands. Default `provider:remote` IDs pass through.
func UnscopeID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	for _, prefix := range []string{"signal-group/", "signal-group:", "whatsapp/", "whatsapp:", "signal/", "signal:", "messages/", "messages:"} {
		if strings.HasPrefix(id, prefix) {
			rest := strings.TrimPrefix(id, prefix)
			if strings.HasSuffix(prefix, "/") {
				if i := strings.IndexByte(rest, '/'); i >= 0 {
					return rest[i+1:]
				}
			}
			return rest
		}
	}
	return id
}

// RiverIDFromScoped extracts the extra-river id from a namespaced conversation
// id. Default-form ids return "".
func RiverIDFromScoped(id string) string {
	id = strings.TrimSpace(id)
	for _, prefix := range []string{"signal-group/", "whatsapp/", "signal/", "messages/"} {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		rest := strings.TrimPrefix(id, prefix)
		if i := strings.IndexByte(rest, '/'); i > 0 {
			return rest[:i]
		}
	}
	return ""
}
