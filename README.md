# go-chat

A concurrent TCP chat server written in Go.

## Running

```bash
go run server.go
```

Connects on port `9000`. Use `telnet` or `nc` to join:

```bash
nc localhost 9000
```

## Commands

| Command | Description |
|---|---|
| `/users` | List connected users |
| `/stats` | Show your message count |
| `/help` | Show available commands |
| `/quit` | Leave the chatroom |

## Design decisions

**Single hub goroutine for shared state.** All mutations to the `clients` map — registration, lookup, deletion — go through a single `hub()` goroutine via channels (`msgChan`, `registerChan`, `leaveChan`, `userListChan`). This avoids mutexes entirely and makes the concurrency model easy to reason about: only one goroutine ever touches shared state.

**Per-client write goroutine.** Each client gets a dedicated `writeLoop()` goroutine that drains a buffered `writeChan`. Writing to a TCP connection blocks; isolating it per-client means a slow or stalled client doesn't block the hub or other clients' message delivery.

**Channel-typed requests with reply channels.** Operations that need a response (e.g. registering a username, listing users) use a request struct with an embedded `reply chan` field. The caller blocks on the reply channel after sending the request, and the hub responds directly. This keeps request/response semantics clean without callbacks or shared memory.

**`BroadcastType` enum.** The `Message` struct carries a `BroadcastType` (ToAll, ToSender, ToAllButSender, ToUser) rather than having separate channel types for each delivery pattern. The `broadcast()` function switches on this field, making it easy to add new delivery modes.

## Planned features

- **Direct messages** — `/dm <user> <message>` using the existing `ToUser` broadcast path, which is already wired but not exposed as a command
- **Message history** — replay the last N messages to clients on join, stored as a ring buffer in the hub
- **Chat rooms** — `/join <room>` and `/leave`, with broadcasts scoped to room membership
- **Ping / keepalive** — periodic writes to detect and evict stale connections
- **Rate limiting** — per-client token bucket to prevent message spam
- **Admin commands** — first-connected client gets operator status; `/kick <user>` and `/ban <user>`
