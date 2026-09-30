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
- **One queue per replica** (added after this decision). Each API process consumes
  notifications from its own queue, `dozlab.events.dozlab-api.instance.<hostname>-<random>`, so
  every replica receives every notification and delivers it to the sockets it holds. It is a
  non-durable classic queue with `x-expires` of 2 minutes rather than an exclusive queue, so it
  survives a reconnect to the broker but is deleted once its process is gone. The sockets die
  with the process anyway, so there's nothing to keep.

---

# Decision: who creates the per-session writable disk

- **Status:** accepted (2026-09-30), to be reviewed once built (see [Time](#time))
- **Decision:** option A, the init container (`dozlab-rootfs-manager/init-setup/init.sh`).
  A and B are expected to save the same time, so `init.sh` wins on keeping disk setup in one place.

## In short

**What we're changing:** today every lab session gets its own full copy of the VM's disk, which
is then checked and grown to 4 GB. The plan is for every session to boot from one shared,
read-only base disk and write its changes to a small disk of its own. Everything a session
changes (files the student makes, its SSH key, its hostname) goes to that small disk.

**The question:** which part of the system creates that small writable disk for each session?

### A. The init container (`init.sh`), which runs just before the VM starts

**Choose it if** you want all disk preparation in one place. `init.sh` already gets the disk
ready, grows it and (with rootfs-manager #9) writes the cloud-init seed. It would now also create
the small disk and put the seed on it.

- **You get:** one image and one script to understand and debug. The Firecracker image stays
  generic. It fits the earlier decision that `init.sh` owns disk sizing.
- **You give up:** the disk goes away when the pod does, so a restarted session starts fresh.
  That's the same as today.

### B. The Firecracker container (`start-firecracker.sh`), which runs the VM

**Choose it if** you'd rather the component that starts the VM also make the disk it attaches,
with no handoff between containers.

- **You get:** one less step between two containers.
- **You give up:** disk setup is split across two images in two repos (rootfs-manager and
  dozlab-infra). The Firecracker image, which knows nothing about sessions today, would have to
  learn about per-session sizes and cloud-init seeds. It also goes against the decision that
  `init.sh` owns sizing.

### C. The controller, which asks Kubernetes for a disk volume

**Choose it if** you want sessions to **survive a pod restart**, so a student's work and the VM's
identity are still there when the pod comes back. That also opens the way to pausing and
resuming sessions later.

- **You get:** persistence, with the storage managed by Kubernetes.
- **You give up:** it doesn't work on the local cluster today, because the local-path storage
  provisioner can't give out raw disk volumes. It needs a different storage setup. A brand-new
  volume still has to be formatted and seeded by something, so A or B is built as well.

### How to decide

Ask: **"Does a lab session need to survive its pod?"**

- **No, a session is disposable** (as it is today): pick **A**. It's the simplest and matches
  the earlier decisions, and B only adds complication.
- **Yes, students should be able to come back to their work:** that needs **C**. Even then,
  build **A** first, because C needs A's formatting and seeding code, and C can be added later
  once there is storage that supports it.

That's why the recommendation is **A now**, with C as a possible later step.

## Time

**All three options save about the same time.** The time is saved by switching to a shared
read-only base, and that doesn't depend on who creates the small disk.

- **Where the time goes today:** the init container copies the whole disk, checks it and grows it
  to 4 GB. That takes 3.1 s for the vm lab and 12.6 s for the k8s lab (measured 2026-09-30,
  `docs/lab-timings.md` in rootfs-manager #8). With a read-only base, the checking and growing
  go away.
- **Creating the small disk costs about the same in A and B.** It's an empty file plus
  `mkfs.ext4` on a nearly empty disk, which should take well under a second. Not measured yet.
- **C is slower for a new session and faster for a restarted one.** Kubernetes has to create and
  attach a volume before the pod can start, which adds time. On a restart the disk is already
  there. cloud-init also recognizes the VM and skips first-boot setup such as making new host
  keys, part of why boot → SSH went from 2.4 s to 5.6 s with cloud-init (rootfs-manager #9).

Caveats:

1. **The copy only goes away if pods share the base.** If each pod still copies the base into its
   own space, the checking and growing are saved but not the copy. That is the separate decision
   in [Sharing the base](#separate-decision-sharing-the-base), and it matters most for the
   1.4 GB k8s lab.
2. **None of the options removes cloud-init's ~3 s at boot.** Only C avoids it, and only on
   restarts.

Time isn't a reason to pick one option over another, so A was chosen for the reasons above.

**Review later:** once A is built, measure the init container and boot → SSH for the vm and k8s
labs again and compare them with the numbers above. That confirms the time saving is real, both
in creating the writable disk (`init.sh`) and in the VM boot with the overlay
(`start-firecracker.sh` and `overlay-init`). If the base isn't shared by then, measure with and
without sharing so the copy's share is visible. Record the results here and in
rootfs-manager's `docs/lab-timings.md`.

## Context

Today every lab session gets its own full copy of the VM root filesystem:

1. The init container moves the baked `image.ext4` into the pod's `vm-kernels` emptyDir, then
   runs `e2fsck` and `resize2fs` to grow it to the session's disk size
   (`dozlab-rootfs-manager/init-setup/init.sh`).
2. `start-firecracker.sh` (dozlab-infra) attaches that file as the single drive `rootfs`,
   read-write.
3. DozLab/dozlab-rootfs-manager#9 (open) writes the session's cloud-init seed straight into that rootfs with
   `debugfs`.

The controller already creates a PVC per session, `vm-data-<id>`, mounted in the Firecracker
container at `/vm-data`. Nothing uses it as a VM disk yet.

The next speed-up is to stop preparing a full disk per session: boot every session from one
**read-only base disk** and give each session a small **writable disk** for its changes. This
document decides which component creates that writable disk.

### What stays the same in every option

The guest side is shared by all options. A small `overlay-init` in rootfs-manager's `labs/vm_lab`:

1. mounts `/dev/vda` (the read-only base) as the overlay's lower layer
2. mounts `/dev/vdb` (the writable disk) and uses it for the upper and work directories
3. switches root to the overlay and runs `exec /sbin/init`, so systemd starts as it does today

dozlab-infra's `start-firecracker.sh` boots with `init=/sbin/overlay-init` and attaches two drives: the base
with `is_read_only: true` and the writable disk read-write.

## Options

### A. The init container creates it (`init.sh`) (recommended)

`init.sh` creates a sparse ext4 file next to the base, sized to the session's disk. The seed
directory can be put on it at creation time with `mkfs.ext4 -d`, which replaces the `debugfs`
edits in rootfs-manager #9.

| Pros | Cons |
|---|---|
| All disk preparation stays in one image: base delivery, sizing and the cloud-init seed | Per pod: the writable disk is gone when the pod goes, just like today's disk |
| Keeps the owner's decision that `init.sh` owns resizing: sizing becomes creating this file at the right size, with no `resize2fs` on the base | The init container and `start-firecracker.sh` must agree on the file's path (one more env var, like `ROOTFS_PATH` today) |
| The base is never written, so it can later be shared between pods | |
| `mkfs.ext4 -d` writes the seed without `debugfs`, a mount or privileges | |

### B. The Firecracker container creates it (`start-firecracker.sh`)

`start-firecracker.sh` makes the file right before it boots the VM.

| Pros | Cons |
|---|---|
| One less handoff between containers; the init container only delivers the base | Disk setup is split across two images and two repos (rootfs-manager and infra) |
| | The infra image, which is generic today, needs `mkfs.ext4` and has to know about per-session sizes and seeds |
| | Goes against the decision that `init.sh` owns resizing |

### C. The controller provisions it as a block PVC

The controller sets the `vm-data` PVC (or a new one) to `volumeMode: Block` and passes the
device to Firecracker as `/dev/vdb`.

| Pros | Cons |
|---|---|
| The only option where the writable disk **survives the pod**: a basis for resumable sessions and snapshots | Needs a storage class that supports raw block volumes; the local-path provisioner used on the local cluster doesn't |
| Storage size and class are managed by Kubernetes | Firecracker needs the block device inside its container (device access, not a file) |
| | A new device still has to be formatted and seeded by something, so A or B is needed anyway |

## Where cloud-init comes in

cloud-init's job doesn't change: per-session setup inside the VM (the session's SSH key for
root, the hostname, and new SSH host keys). What changes is where its seed and its state live.

**Before boot (host side).** Whichever component creates the writable disk also writes the
NoCloud seed, because the base is read-only and must stay identical for every session. The seed
goes into the writable disk's upper directory at `var/lib/cloud/seed/nocloud/` (`meta-data` with
`instance-id: dozlab-<SESSION_ID>`, `user-data` with the key). Through the overlay, the VM then
sees it at `/var/lib/cloud/seed/nocloud/`, the path rootfs-manager #9 already reads.

**During boot (inside the VM), in this order:**

1. The kernel runs `overlay-init`, which mounts the overlay (base + writable disk) and runs
   `exec /sbin/init`. cloud-init does not run yet.
2. systemd starts. cloud-init's stages run as systemd units: `cloud-init-local` finds the NoCloud
   seed, then `cloud-init` and `cloud-config` apply it (authorized key, hostname, host keys).
3. sshd starts **after** cloud-init (as in rootfs-manager #9), so the session key and new host keys are in place
   when SSH comes up and the pod's startup probe succeeds.

Everything cloud-init writes (`/root/.ssh/authorized_keys`, `/etc/hostname`,
`/etc/ssh/ssh_host_*`, its own state under `/var/lib/cloud/instances/`) lands on the writable
disk. The base never changes.

**What this means for each option:**

- **A and B:** the disk is new for every pod, so cloud-init runs its first-boot setup every time.
  That's the behaviour rootfs-manager #9 has today.
- **C:** on a restart the writable disk still holds cloud-init's state for the same instance id,
  so cloud-init treats it as a reboot: the key and host keys stay the same.
- **In all options:** the base image must not contain cloud-init state under
  `/var/lib/cloud/instances/` from the image build, or cloud-init could treat a new session as
  an old instance.

**Alternative for the seed:** a separate small `cidata`-labelled drive (vfat or ISO) as a third
drive. NoCloud also reads seeds from such a drive. That keeps the seed off the writable disk, but
it's one more file and one more drive per session, so it isn't recommended here.

**rootfs-manager #9 has to change for any option:** it writes the seed into the rootfs with `debugfs`. With a
read-only base, that write moves to the writable disk (or a `cidata` drive).

**Cost:** rootfs-manager #9 measured boot → SSH at 2.4 s before cloud-init and 5.6 s with it (vm lab). The
writable disk doesn't change that; cloud-init's startup time is a separate open item in rootfs-manager #9.

## Separate decision: sharing the base

A read-only base only saves the copy if pods share it. If each pod still copies the base into
its own emptyDir, the writable disk removes the `e2fsck` and `resize2fs` time but not the copy.
Sharing needs a node-level cache (hostPath, or a DaemonSet that pre-pulls it) or Kubernetes
image volumes that mount the init image read-only. That choice matters more for the k8s lab
(1.4 GB rootfs) than for the vm lab (311 MB), and it is decided separately.

## Consequences

- rootfs-manager: `labs/vm_lab` gets `overlay-init`; `init-setup/init.sh` creates the writable
  disk with the seed and stops resizing the base; rootfs-manager #9's `debugfs` seed write is replaced.
- dozlab-infra: `start-firecracker.sh` attaches the base read-only plus the writable disk, and
  boots with `init=/sbin/overlay-init`.
- dozlab-controller: passes the writable disk size to the init container in place of today's
  `IMAGE_SIZE`; the `vm-kernels` emptyDir size limit follows the new layout.
- Option C can be added later for sessions that persist, formatted and seeded by the same code.
