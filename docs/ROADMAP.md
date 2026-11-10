# om-tui Tracking Roadmap & Triage

This document tracks all triaged issues, missing features, upstream fixes, and cross-platform gaps identified during the November 2026 repository audit. It serves as our live backlog and implementation tracker.

---

## 1. Triage Summary Matrix

| ID | Priority | Subsystem | Title / Upstream Reference | Status | Impact / Value |
|---|---|---|---|---|---|
| **P0-1** | **P0** | Google | Validate send recipient against stale thread bindings (Upstream #271) | `Completed` | Prevents misdirecting sends after phone swap/restore |
| **P0-2** | **P0** | Outbox | Outbox lease failure head-of-line unblocking (Upstream #278, #274) | `Completed` | Prevents 1 failed lease from freezing all outbox sends |
| **P0-3** | **P0** | WhatsApp | Fix WhatsApp disconnect message loss race during teardown (Upstream #236) | `Completed` | Prevents lost messages during disconnect/reconnect |
| **P0-4** | **P0** | Media | Handle late-named Google media without permanent failure (Upstream #263) | `Completed` | Prevents images from getting stuck as "Image unavailable" |
| **P1-1** | **P1** | Storage/Perf | Optimize FTS indexing to skip unchanged message updates (Upstream #273) | `Completed` | 99% reduction in message write latency (15ms -> 0.08ms) |
| **P1-2** | **P1** | Google | Auto-retry failed Google cookie repairs with backoff (Upstream #260) | `Completed` | Eliminates false `Blocked` supervisor parks |
| **P1-3** | **P1** | Signal | Drain Signal receive calls stopped mid-download (Upstream #259) | `Completed` | Prevents corrupted/dropped attachments on 5s timeout |
| **P1-4** | **P1** | Signal | Mark missing Signal attachments as 410 & enable import (Upstream #258) | `Completed` | Accurate attachment availability state and manual recovery |
| **P1-5** | **P1** | Outbox | Bound transport calls to 60s in dispatcher (Upstream #248) | `Completed` | Prevents outbox dispatch lockups on hanging transports |
| **P2-1** | **P2** | WhatsApp | Stop storing WhatsApp LIDs as phone numbers & repair V2 (Upstream #256) | `Completed` | Stops identity corruption and synthetic number pollution |
| **P2-2** | **P2** | WhatsApp | Match group read receipt sender form to incoming message (Upstream #255) | `Completed` | Read receipt sender consistency |
| **P2-3** | **P2** | Google | Hold Google intents during phone silence & back off (Upstream #254) | `Completed` | Eliminates CPU/request churn when phone is offline |
| **P2-4** | **P2** | Google | Detect and alert when SMS stops while RCS still flows (Upstream #191, #197) | `Completed` | Early warning of carrier/transport desync |
| **P3-1** | **P3** | Platform | Implement Linux desktop notifications via `notify-send` | `Completed` | Closes notification gap on Linux |
| **P3-2** | **P3** | Platform | Implement macOS/Linux native file pickers for `Ctrl+A` | `Completed` | Replaces stub with `osascript` / `zenity` |
| **P3-3** | **P3** | Platform | Implement non-Windows clipboard image paste for `Ctrl+V` | `Completed` | Enables image clipboard paste via `pbpaste` / `wl-paste` |
| **P3-4** | **P3** | Platform | Smoke-test Keychain (macOS) & Secret Service (Linux) vault backends | `Open` | Runtime validation of credential sealing on real hardware |
| **P4-1** | **P4** | TUI/UX | Activate Klipy GIF search & send in TUI (`>gif <query>`) | `Completed` | Surfaces dormant `/api/gifs` endpoints in TUI |
| **P4-2** | **P4** | TUI/UX | Render Link Previews in chat thread view | `Completed` | Surfaces dormant `/api/link-preview` extraction |
| **P4-3** | **P4** | TUI/UX | Add inline quote / reply-to for Google, WhatsApp, Signal | `Completed` | Brings Slack-like `Ctrl+T` / quote reply to all rivers |
| **P4-4** | **P4** | TUI/UX | Expand emoji reaction palette beyond fixed digits 1–9 | `Completed` | Allows arbitrary reaction emojis |
| **P4-5** | **P4** | TUI/UX | Persistent drafts across river switching and restarts | `Completed` | Prevents losing in-progress composer text |

---

## 2. Detailed Findings & Technical Breakdown

### Category 1: Safety & Data Integrity (P0)

#### P0-1: Validate Send Recipient Against Stale Thread Bindings (Upstream #271 - `b532b7a`)
- **Problem:** Google thread IDs are device-local. When a phone is replaced, restored from backup, or updated, thread IDs can be reused for different people. If the local store retains an outdated mapping, an outgoing text, media, or reaction could be sent to the wrong person.
- **Upstream Fix:** Upstream added `ingest.CheckSendTarget`, comparing the phone's current thread participants against the stored roster before sending. Any mismatch rejects the send with terminal class `recipient_mismatch` instead of sending to the wrong person.
- **Affected Packages:** `internal/bridgeadapters/google/send.go`, `internal/ingest/idspace.go`, `internal/bridge/contracts.go`.

#### P0-2: Outbox Lease Failure Head-of-Line Unblocking (Upstream #278 - `017ff35`, #274 - `3430324`)
- **Problem:** In the V2 outbox, `LeaseDue` leases due rows in a single batch transaction. If a single row fails to lease (e.g. database constraint, schema anomaly, or corrupt field), the entire batch is rolled back. In the next dispatch loop, the exact same failing row is evaluated first, halting all outgoing sends across all accounts.
- **Upstream Fix:** Introduced `LeaseDueBatch` with individual savepoints per row. Failing rows are marked `not_dispatched` (`lease`) and set aside with exponential delay (doubling up to 5 min), allowing healthy sends to proceed immediately.
- **Affected Packages:** `internal/storage/sqlite/outbox.go`, `internal/messaging/dispatch.go`, `internal/messaging/dispatch_pass.go`.

#### P0-3: WhatsApp Disconnect Message Loss Race (Upstream #236 - `d31bb83`)
- **Problem:** When a WhatsApp connection generation is torn down (e.g., network drop or sleep/wake), the context passed to `whatsmeow` handlers is cancelled. If a message is mid-decryption, SQLite statements fail after committing the ratchet advance, causing the server redelivery to fail with "old counter" and drop the message permanently.
- **Upstream Fix:** Detaches handler context from generation teardown, opens session store with `_txlock=immediate`, and uses whatsmeow's decrypted event buffer so decrypt and frame commit occur atomically.
- **Affected Packages:** `internal/whatsapplive/client.go`, `internal/bridgeadapters/whatsapp/whatsapp.go`.

#### P0-4: Handle Late-Named Google Media (Upstream #263 - `a455f63`)
- **Problem:** Google Messages sends image message metadata ~1–2 seconds before naming the actual media ID in a subsequent update. If the user or TUI requests the image immediately upon receiving the message, `/api/media` fails with 501, and the UI latches "Image unavailable" permanently.
- **Upstream Fix:** Returns `bridge.ErrMediaNotReady` mapped to HTTP 503 `Retry-After: 2`. Message reads carry `media_version` hash, enabling automatic refresh once the media ID arrives without breaking existing rows.
- **Affected Packages:** `internal/bridgeadapters/google/`, `internal/media/`, `internal/web/api.go`.

---

### Category 2: Reliability & Performance (P1)

#### P1-1: Optimize FTS Indexing to Skip Unchanged Message Saves (Upstream #273 - `81be93f`)
- **Problem:** Every save to `messages.db` unindexed and reinserted into `messages_fts`. Because `message_id` is unindexed in SQLite FTS, each delete scanned all rows (~80,000 index rows, ~15ms per save). Periodic Signal/WhatsApp rescans triggered massive disk I/O and rebuilding cycles.
- **Upstream Fix:** Only update `messages_fts` when message text actually changes. Drops write latency from ~15ms down to ~0.08ms.
- **Affected Packages:** `internal/db/messages.go`, `internal/importer/signal.go`.

#### P1-2: Auto-Retry Failed Google Cookie Repairs (Upstream #260 - `9aacf98`)
- **Problem:** A single transient network failure during Chrome cookie refresh parked the Google supervisor in `Blocked` state with no retry mechanism, requiring a complete daemon restart.
- **Upstream Fix:** Classify repair errors, only parking on genuine terminal auth rejections. Transient errors retry with exponential jitter up to `MaxBackoff`.
- **Affected Packages:** `internal/bridge/supervisor.go`, `internal/bridgeadapters/google/`, `cmd/serve.go`.

#### P1-3: Drain Signal Receive Calls Stopped Mid-Download (Upstream #259 - `0fa06ad`)
- **Problem:** When `signal-cli receive` reaches its 5s timeout, a SIGTERM is sent. signal-cli continues downloading attachments before exiting. om-tui previously killed the process with SIGKILL after 3s, interrupting downloads and discarding the message.
- **Upstream Fix:** Implemented `receive_drain.go` to monitor active attachment growth up to 30s before issuing a hard kill.
- **Affected Packages:** `internal/signallive/client.go`, `internal/signallive/receive_drain.go`, `internal/signallive/command_gate.go`.

#### P1-4: Missing Signal Attachment Handling & Import (Upstream #258 - `f068c31`)
- **Problem:** If signal-cli failed to store an attachment, requests hung or returned generic errors.
- **Upstream Fix:** Explicitly mark unstored Signal attachments as 410 Gone with a persisted failure reason, allowing manual import/replacement of missing media.
- **Affected Packages:** `internal/signallive/`, `internal/media/`.

#### P1-5: Bound Transport Calls in Dispatcher (Upstream #248 - `104ef99`)
- **Problem:** An unanswered transport call could hold dispatcher workers indefinitely.
- **Upstream Fix:** Wrapped every outbox transport call in a strict 60s context deadline.
- **Affected Packages:** `internal/messaging/transport_call.go`, `internal/messaging/dispatch.go`.

---

### Category 3: Data Hygiene & Upstream Polish (P2)

#### P2-1: WhatsApp LID Identity Normalization (Upstream #256 - `65f0d64`)
- **Problem:** WhatsApp LIDs (`<digits>@lid`) were converted into synthetic phone numbers (`+<digits>`), polluting contact records and group read receipts.
- **Upstream Fix:** Store LIDs as distinct JIDs; added `om-tui repair whatsapp-lid` command to rewrite corrupted V2 identities back to clean JID/E.164 mappings.
- **Affected Packages:** `internal/importer/whatsapp.go`, `internal/ingest/`, `cmd/repair.go`.

#### P2-2: WhatsApp Group Read Receipt Sender Matching (Upstream #255 - `4a4f710`)
- **Problem:** Read receipt sender JID format (LID vs phone) did not consistently match how messages arrived.
- **Upstream Fix:** Align read receipt sender addresses with incoming message format.
- **Affected Packages:** `internal/bridgeadapters/whatsapp/`.

#### P2-3: Google Silent Phone Intent Holding (Upstream #254 - `23fa0fa`)
- **Problem:** Repeated sends and queries against an offline/silent phone churn CPU, connection buffers, and logs.
- **Upstream Fix:** Temporarily hold Google intents in backoff windows (30s doubling to 5m) when phone is silent.
- **Affected Packages:** `internal/bridgeadapters/google/phone_silence.go`, `internal/app/gm_failfast.go`.

#### P2-4: SMS Silence vs RCS Flow Sentinel (Upstream #191, #197)
- **Problem:** A phone can stay connected on RCS data while cellular SMS reception is dead, causing silent outbound SMS failures.
- **Upstream Fix:** Added heuristic detection and `/api/status` alerting when SMS has been silent significantly longer than RCS baseline.
- **Affected Packages:** `internal/app/`, `internal/web/api.go`.

---

### Category 4: Cross-Platform Parity (P3)

#### P3-1: Linux Desktop Notifications
- **Problem:** Windows has rich toast notifications via PowerShell AUMID. macOS has basic osascript/terminal-notifier. Linux has **zero** desktop notification support.
- **Solution:** Add `internal/notify/linux.go` using `notify-send` (or D-Bus `org.freedesktop.Notifications`).
- **Affected Packages:** `internal/notify/linux.go`, `internal/notify/common.go`.

#### P3-2: Native File Pickers for macOS / Linux (`Ctrl+A`)
- **Problem:** `internal/tui/attach_stub.go` returns an error for all non-Windows systems, forcing users to type absolute paths.
- **Solution:**
  - macOS: invoke `osascript -e 'POSIX path of (choose file)'`.
  - Linux: invoke `zenity --file-selection` or `kdialog --getopenfilename`.
- **Affected Packages:** `internal/tui/attach_darwin.go`, `internal/tui/attach_linux.go`, `internal/tui/attach_stub.go`.

#### P3-3: Clipboard Media Extraction on Non-Windows (`Ctrl+V`)
- **Problem:** Clipboard file and image extraction is currently Windows-only (`attach_windows.go`).
- **Solution:**
  - macOS: `osascript` / `pbpaste` to extract image clipboard data to temp PNG.
  - Linux: `wl-paste` (Wayland) / `xclip -selection clipboard -t image/png` (X11).
- **Affected Packages:** `internal/tui/attach_darwin.go`, `internal/tui/attach_linux.go`.

#### P3-4: Runtime Smoke-Testing of macOS & Linux Vaults
- **Problem:** macOS Keychain (`security`) and Linux Secret Service (`secret-tool`) are compiled but haven't been validated on hardware.
- **Solution:** Set up automated or manual verification instructions to test Slack/river token sealing on real macOS and Linux machines.
- **Affected Packages:** `internal/vault/seal_darwin.go`, `internal/vault/seal_linux.go`.

---

### Category 5: TUI Polish & Features (P4)

#### P4-1: Activate Klipy GIF Search in TUI
- **Problem:** [`internal/web/gifs.go`](file:///c:/Users/whobs/dev/om-tui/internal/web/gifs.go) has full Klipy API client and search logic, but it is unused by the TUI.
- **Solution:** Add a palette action (`>gif <search terms>`) or modal to browse GIF titles/preview URLs and send directly.
- **Affected Packages:** `internal/tui/palette.go`, `internal/tui/model.go`.

#### P4-2: Render Link Previews in Chat Threads
- **Problem:** [`internal/web/linkpreview.go`](file:///c:/Users/whobs/dev/om-tui/internal/web/linkpreview.go) extracts OpenGraph metadata (title, description, domain), but the TUI ignores it.
- **Solution:** Render a clean 1-2 line summary box beneath messages containing URLs.
- **Affected Packages:** `internal/tui/model.go`, `internal/tui/wrap.go`.

#### P4-3: Cross-River Inline Quote / Reply-To
- **Problem:** Slack river supports `Ctrl+T` for thread replies, but Google Messages, WhatsApp, and Signal only support sending to the general channel.
- **Solution:** Add `r` or `Ctrl+Q` on a highlighted message in thread view to quote-reply.
- **Affected Packages:** `internal/tui/model.go`, `internal/messaging/reply_ref.go`.

#### P4-4: Expanded Emoji Reaction Picker
- **Problem:** `Ctrl+E` opens a fixed 1–9 emoji bar (`1👍 2❤️ ...`).
- **Solution:** Allow typing arbitrary emoji characters or searching via `:emoji:` shortcodes.
- **Affected Packages:** `internal/tui/reactions.go`.

#### P4-5: Persistent Drafts
- **Problem:** Drafts in composer are stored in-memory and can be lost on sudden terminal exit.
- **Solution:** Persist active composer text per conversation to the V2 `drafts` table or local cache.
- **Affected Packages:** `internal/tui/model.go`.

---

## 3. Recommended Phased Execution Plan

### Phase 1: Critical Safety & Outbox Stability (Completed)
- [x] **P0-1:** Port Google send recipient validation (Upstream #271).
- [x] **P0-2:** Port outbox lease failure isolation & retry (Upstream #278, #274).
- [x] **P0-4:** Port Google late media 503 retry (Upstream #263).
- [x] **P1-1:** Port SQLite FTS indexing optimization (Upstream #273).
- [x] **P1-4:** Port missing Signal attachment 410 & import (Upstream #258).
- [x] **P1-5:** Bound transport calls in outbox worker to 60s (Upstream #248).
- [x] Route v2-primary reactions through the outbox (Upstream #227).

### Phase 2: Transports & Connection Recovery (Completed)
- [x] **P1-2:** Port Google cookie repair auto-recovery with backoff (Upstream #260).
- [x] **P1-3:** Port Signal receive download drain (Upstream #259).
- [x] **P0-3:** Port WhatsApp disconnect message loss race fix (Upstream #236).
- [x] **P2-1:** Port WhatsApp LID identity cleanup and repair command (Upstream #256).
- [x] **P2-2:** Port WhatsApp group read receipt sender matching (Upstream #255).

### Phase 3: Cross-Platform Parity
- [x] **P3-1:** Implement Linux desktop notifications (`notify-send`).
- [x] **P3-2:** Implement macOS & Linux file pickers (`osascript` & `zenity`).
- [x] **P3-3:** Implement macOS & Linux clipboard media paste.
- [ ] **P3-4:** Runtime smoke-testing of macOS & Linux vaults (Keychain & Secret Service).

### Phase 4: TUI Feature Enhancements (Completed)
- [x] **P4-1:** Wire `>gif` palette search command to Klipy API client.
- [x] **P4-2:** Render OpenGraph link previews in chat thread view.
- [x] **P4-3:** Implement inline quote-reply (`r` / `Ctrl+Q`) for Google Messages, WhatsApp, Signal.
- [x] **P4-4:** Expanded emoji reaction picker with `:shortcode:` and unicode glyph resolution.
- [x] **P4-5:** Wire persistent drafts across river switching and restarts.
