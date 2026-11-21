# Private messages

Private messaging uses Cloudy's existing MyGo and `golang.org/x/image/webp`
dependencies for its interface and media, adding no new third-party dependency.
All private-message reads and mutations run over HTTPS using Cloudy's existing
WEAPI/EAPI transport.

Signed-in listeners open **私信** between **我的主页** and **设置…** in the account
menu, or **发私信** on another user's page. The independent panel follows the
settings pane's dimensions/material/transition. Its header says only **私信**;
the selected contact name appears in the chat header. Opening the panel
refreshes recent conversations once and offers a retry on error. Unread badges
belong to individual conversations. Automatic inbox work runs only while the
panel is open.

Search filters loaded contact nicknames locally. Conversation/history pagination
is explicit; unsupported or malformed message payloads display a placeholder.
Drafts and scroll positions are separate for each contact. Text is limited to
1000 characters, and sends are serial per conversation. Sending immediately
moves the text into a local outgoing bubble. A successful response triggers
history reconciliation. A timeout or closing an in-flight send leaves a local
record with an uncertain result. Refresh history before retrying because the
server may have accepted it. Cloudy never retries a send automatically.

**Click 发送 or press Enter to send. Shift+Enter inserts a newline.** During IME
composition, Enter commits the composition. Composer and contact-search focus
suppress the player's Space shortcut. Escape and the close button use the
existing dialog behavior.

## Protocol boundary

Wrapper route names are not NetEase HTTP paths. The implementation uses these
internal endpoints:

| Operation | Internal path | Protocol and payload |
| --- | --- | --- |
| Recent conversations | `/api/msg/private/users` | WEAPI; offset, limit=30, total |
| History | `/api/msg/private/history` | WEAPI; userId, time, limit=30, total |
| Send text | `/api/msg/private/send` | EAPI; type=text, msg, userIds JSON array string |
| Mark one conversation read | `/api/communication/msg/unread/count/clean` | EAPI; userId |

Primary implementation references:

- [msg_private.js](https://github.com/neteasecloudmusicapienhanced/api-enhanced/blob/master/module/msg_private.js)
- [msg_private_history.js](https://github.com/neteasecloudmusicapienhanced/api-enhanced/blob/master/module/msg_private_history.js)
- [send_text.js](https://github.com/neteasecloudmusicapienhanced/api-enhanced/blob/master/module/send_text.js)
- [option.js](https://github.com/neteasecloudmusicapienhanced/api-enhanced/blob/master/util/option.js), [request.js](https://github.com/neteasecloudmusicapienhanced/api-enhanced/blob/master/util/request.js), [config.json](https://github.com/neteasecloudmusicapienhanced/api-enhanced/blob/master/util/config.json), [crypto.js](https://github.com/neteasecloudmusicapienhanced/api-enhanced/blob/master/util/crypto.js)

Reads use WEAPI (`encrypt=weapi`) with the snapshotted cookie jar. Sends, read
acknowledgements, and sticker endpoints use Cloudy's existing EAPI cipher and
header/Cookie builder (including its install device ID and client metadata) from
the snapshotted account cookies, sent without attaching the cookie jar. WEAPI
uses standard double AES-CBC/base64 plus reversed-key raw RSA; the WEAPI test
vector was verified independently with Node crypto/OpenSSL. The HTTP transport
is shared with the existing NetEase client, while the cookie jar and cookie list
are snapshotted from the in-memory account when the panel is first opened. This
prevents a queued worker from borrowing a later account's cookies. Redirects are
refused, requests time out after 20 seconds, and panel/account invalidation
cancels them.

The device resolver runs only on the sending worker; offline tests replace it
with a synthetic ID. Code 250 is a server rejection. The local bubble preserves
the failed text and asks the listener to check history before retrying.

Additional send implementations checked:
[Music163Api-Go](https://github.com/XiaoMengXinX/Music163Api-Go/blob/master/api/msgSend.go)
and [MeloX](https://github.com/youshen2/MeloX/blob/main/MeloX/Core/Networking/NeteaseSocialAPI.swift).

Offline fixtures cover request shapes, sending and history reconciliation,
unknown message types, and sticker decoding/rendering.

## HTTPS inbox synchronization and limits

Automatic inbox synchronization runs only while the private-messages panel is
open and an account is signed in. Panel closure, hiding the main window,
account replacement, and app exit cancel timers and in-flight requests and
reject stale responses via account/epoch/request generations.

- **Cadence (candidate parameters)**: When the main window is visible and
  focused, the scheduler checks `Conversations(0)` every 30 seconds; when
  visible but unfocused (e.g., on a secondary display), every 60 seconds; when
  minimized or hidden, automatic polling pauses and in-flight automatic reads
  are cancelled. Restoring (`Window.OnRestore`) and focusing (`Window.OnFocus`)
  share a 5-second throttle and deduplicate against in-flight opens so back-to-
  back wake events dispatch at most one check.
- **Conditional History**: Only when `Conversations(0)` reports a timestamp
  newer than the currently selected conversation's cached history tail (or
  advances an unloaded selected conversation) does the scheduler queue a
  follow-up `History(selected, 0)` request. Non-selected conversations update
  contact metadata and badges without scanning their history.
- **Lagging responses and backoff**: If `Conversations` leads `History`, stale
  pages are downgraded from authoritative pruning so newer cached messages are
  never deleted, and subsequent low-frequency `Conversations(0)` checks re-check
  `History(selected, 0)` until the cached tail catches up. Automatic requests
  share an account-scoped backoff (HTTP/business 429 waits at least 60s,
  escalating up to 5m, and cannot be cleared by older in-flight 200 replies).
  Cloudy never retries `Send` or `SendSticker` automatically.
- **Unread, Read and pagination boundaries**: Server `UnreadKnown=false` is
  never treated as zero; local read boundaries (`readAt` and per-millisecond
  confirmed peer message IDs) stay separate from server unread counts and the
  scrolled-up `"有新消息"` jump prompt. Hot multi-page catch-up fetches at most
  4 pages per pass and sets `gapUntil` whenever an older boundary is not yet
  bridged. Empty or non-advancing older pages cannot clear `gapUntil` or unlock
  whole-conversation `Read`.
- **Known limits**: Because `Conversations` exposes no message ID or snapshot
  version token, consecutive snapshots with identical `Time`, truncated
  `Summary`, and `Unread` cannot guarantee automatic discovery without manual
  refresh or reopening; conversations outside `offset=0` (top 30) are not
  automatically discovered; a cold conversation with more than 30 unread
  messages loads the latest 30 and allows marking read once the tail covers
  `serverUnreadAt`; automatic sync avoids pruning cached messages on ambiguous
  rollback responses, leaving confirmed tail/middle deletion to explicit
  refresh or re-entering the conversation.

## State and privacy

`internal/netease/private_messages.go` owns transport/parsing;
`internal/app/private_messages.go` owns memory-only state;
`internal/app/ui_private_messages.go` owns presentation. The backend factory can
be replaced by tests. Blocking work runs in goroutines; results go through
`a.post`, guarded by account/panel/request generations. Each response targets its
original chat. Account changes discard all contacts, messages, drafts and client
credentials. No private body is logged, persisted or sent to a third-party
service. Errors expose only safe local descriptions/status codes, never server
text that might echo private content. The regular application still uses its
existing account/session storage; this feature adds no persistent chat storage.

Build on Windows x64 using the unchanged official tool:

```powershell
$env:CGO_ENABLED='0'
go test -mod=readonly ./internal/...
go test -mod=readonly ./internal/app -run TestPrivateMessages -count=5
go tool mygo build -platform windows/amd64 -o build/private-messages
```

No updater private key, upload option, release or installation is needed. Keep
the generated `windows-amd64` directory together. Launching the built binary
uses Cloudy's normal configuration/session, so development screenshots use only
`ui.NewTester` and synthetic data (`NM_FRAMES`) instead of launching the app.

## Official private stickers

Cloudy implements the group, detail page, and native image send request shapes
described by [Music Party's protocol notes](https://github.com/tabidachinokaze/music-party/blob/main/docs/OFFICIAL-MULTIPLAYER.md#官方自定义表情0120)
and [its service](https://github.com/tabidachinokaze/music-party/blob/main/src/main/service.ts).
The picker keeps usable built-in and saved groups returned for private messages.
Sticker and picture IDs retain their decimal precision throughout. For native
image delivery the client obtains a fresh token using the service documented in
[API Enhanced's registration module](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/main/module/register_checktoken_v3.js).
That token request carries neither the Cloudy login session nor chat content.

The Go implementation uses Cloudy's existing EAPI transport and MyGo widgets.
These references supplied protocol facts. Their source code, dependencies,
and artwork are not bundled. [Music163Api-Go's image path](https://github.com/XiaoMengXinX/Music163Api-Go/blob/master/api/msgSend.go)
was checked for comparison and uploads ordinary images, so Cloudy uses the
existing server sticker identity instead. Server history supplies the only
bubble for a sent sticker. The picker uses the first frame. Chat bubbles play
GIF, APNG and WebP when motion is enabled and display a still with motion off.
WebP sticker animations that exceed the decoded-frame budget show only the first
composited frame. Sticker downloads and local reads are capped at 16 MiB.
