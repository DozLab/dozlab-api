# Decision: authenticating browser WebSocket connections to `/api/v1/ws`

- **Status:** accepted (2026-09-27)
- **Decision:** option 1, the JWT sent as a `Sec-WebSocket-Protocol` value, with the
  `Authorization` header also accepted

## Context

Notification events published on the RabbitMQ bus (`POST /api/v1/proxy/notifications`) are
consumed by dozlab-api and pushed to the user's open WebSocket connections at `GET /api/v1/ws`.
The rest of the API authenticates with `Authorization: Bearer <JWT>` (`middleware.AuthMiddleware`).

Browsers can't set that header on a WebSocket: `new WebSocket(url, protocols)` only lets the
page choose the URL and the `Sec-WebSocket-Protocol` values. Cookies would work, but the API
doesn't use cookie sessions (the frontend holds the JWT). So the browser has to put the token
either in the URL or in the subprotocol list.

## Options

### 1. Subprotocol token (chosen)

```js
new WebSocket("wss://api.example/api/v1/ws", ["dozlab.bearer", accessToken]);
```

The server reads the token that follows `dozlab.bearer` in `Sec-WebSocket-Protocol` and replies
with `Sec-WebSocket-Protocol: dozlab.bearer` only (never the token). Browsers require the server
to echo one of the offered protocols. `Authorization: Bearer` also works for CLI and service clients.

| Pros | Cons |
|---|---|
| The token stays out of URLs, so it isn't written to access logs, proxy logs or browser history, and isn't leaked through `Referer` | Uses the subprotocol header for something it wasn't designed for; the convention needs documenting (this file, README) |
| Standard browser API, no extra round trip or server state | Some proxies/WAFs log all request headers; that risk is the same as for `Authorization` |
| The same middleware covers browser and non-browser clients | The frontend must send the sentinel `dozlab.bearer` first and handle the echoed protocol |
| Used elsewhere too (e.g. Kubernetes' `base64url.bearer.authorization.k8s.io` subprotocol) | JWTs must stay valid subprotocol tokens (base64url + `.`, which they are) |

### 2. Query parameter `?token=<JWT>`

```js
new WebSocket(`wss://api.example/api/v1/ws?token=${accessToken}`);
```

| Pros | Cons |
|---|---|
| Simplest for the frontend and for manual testing | The JWT ends up in access/proxy/load-balancer logs (gin's logger prints the full path), browser history and monitoring tools; anyone with log access can replay it until it expires |
| Works with every client and proxy | Needs log redaction everywhere the URL is recorded to be safe |
| | Access tokens here are long-lived enough that a leaked one is useful |

### 3. `Authorization` header only (for now)

`/ws` behind the existing `AuthMiddleware` unchanged.

| Pros | Cons |
|---|---|
| No new auth code; the same path as every other route | Browsers can't connect, so the frontend can't receive notifications until a follow-up lands |
| Fine for CLI clients, services and tests | Moves the decision later instead of making it |

### Not chosen, for completeness

- **Short-lived ticket:** `POST /api/v1/ws/ticket` (header-authenticated) returns a single-use,
  ~30 s ticket stored in Redis, and the browser connects with `?ticket=`. It's the most robust
  against leaks (a logged ticket is already spent), but it adds an endpoint, Redis state and a
  round trip per connect. Worth revisiting if the API moves to multiple replicas behind a public
  load balancer.
- **Cookie session:** the API has no cookie auth. Adding it for one route brings CSRF and
  cross-site WebSocket hijacking concerns (`CheckOrigin` currently allows every origin).
- **Authenticate in the first message after connecting:** it works, but the server has to accept
  unauthenticated sockets and time them out, and `Manager` registers clients by user at connect time.

## Consequences

- `middleware.WebSocketAuthMiddleware` accepts `Authorization: Bearer <JWT>` or
  `Sec-WebSocket-Protocol: dozlab.bearer, <JWT>`. Missing or invalid token → 401 before the upgrade.
- The upgrader only offers the `dozlab.bearer` subprotocol, so it echoes that and never the token.
- `CheckOrigin` still allows every origin. With token (not cookie) auth, a cross-site page can't
  act as the user without the token, so this isn't a hijacking risk today. It should be tightened
  to the frontend's origins before production anyway.
- The token is only checked at connect time. A connection stays open after the token expires;
  if that matters, the server should close sockets at the token's `exp`.

## Related decisions in the same change (not about auth)

- **Delivery is best-effort to connected clients.** If the user has no open connection, the
  consumer acks and drops the notification. There's no inbox or replay; storing notifications
  would be a separate feature.
- **Single replica.** Every API replica shares the queue `dozlab.events.dozlab-api`, so each
  notification reaches only one replica. Users connected to the other replicas miss it. With
  more than one replica, each replica needs its own queue (e.g. an exclusive per-instance queue
  bound to `notification`) or a shared connection registry.
