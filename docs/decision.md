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

## Persistent and non-persistent sessions

**The owner wants both (2026-09-30):** a session can be **non-persistent** (its disk is gone
with the pod, as today) or **persistent** (the student can stop and continue where they left
off). Option A supports both. The only difference is where `init.sh` puts the writable disk.

> **Before you ask for a persistent session, know what it costs.**
> - **A longer wait to start.** A persistent session needs its own storage volume, and the
>   session can't be placed on a node until that volume is ready. A non-persistent session
>   skips this and starts fastest. (Measured so far: 4–22 s to schedule with volumes vs about
>   1 s without; see [Startup time](#startup-time).)
> - **A wait when you stop.** Saving running programs (a snapshot) writes the VM's whole memory
>   to disk, 1 GiB today, when the session is paused.
> - **Resume isn't instant without a snapshot.** Without one, your files are kept but the VM
>   boots again and your running programs are gone.
> - **It's tied to one machine.** On the local cluster your saved session can only come back on
>   the node that stored it. If that node is busy or down, you wait.
> - **It stays on an old image.** Your session keeps the base image it started on and doesn't
>   get updates to it.
> - **It uses storage until it's deleted.** Paused sessions are removed after a cleanup period.
>
> If you don't need to come back to your work, choose non-persistent.

| | Non-persistent | Persistent |
|---|---|---|
| Where the writable disk lives | the pod's `vm-kernels` emptyDir | the session's `vm-data` PVC |
| What survives the pod | nothing | the student's files; with a snapshot, also running programs |
| `init.sh` | always creates a new disk with the seed | creates it only if it isn't there yet; otherwise reuses it |
| Works on the local cluster | yes | yes: `vm-data` is a normal filesystem volume on local-path. C's raw block volume isn't needed |

### Two levels of "continue where they left off"

1. **Keep the disk, cold boot on return.** The files are kept, and running programs are not. The
   VM boots normally on the kept disk. cloud-init sees the same instance id, treats it as a reboot,
   and keeps the SSH key and host keys. This needs no snapshot code.
2. **Keep the disk and a snapshot.** On pause, Firecracker writes the VM's memory and CPU state
   to the PVC next to the disk, then the pod is deleted. On resume, it restores from them, so
   running programs continue too. `start-firecracker.sh` already has the snapshot create and
   load calls (`SNAPSHOT_PATH`, `MEM_FILE_PATH`).

Level 1 can ship first. Level 2 builds on it.

### Startup time

- **Having the option costs nothing at startup.** It's a choice of path in `init.sh` and the
  controller.
- **Non-persistent is the fastest mode.** The disk is on node-local emptyDir, and the controller
  can skip the `vm-data` PVC for these sessions. That matters because every session creates two
  PVCs today (`vm-data`, 10 Gi, and `vscode-data`, 5 Gi), and local-path is `WaitForFirstConsumer`:
  the pod isn't scheduled until the volumes are provisioned. In the API e2e runs on 2026-09-30,
  "pod scheduled" took 4–22 s with the two PVCs. The `dozlab.sh` test pod, which has only
  emptyDirs, took about 1 s. That gap hasn't been split up yet, so some of it may not be the PVCs.
- **Persistent, first start:** the same as today, because the `vm-data` PVC is already created
  for every session. The writable disk on local-path is a directory on the node's disk, so it's
  about as fast as emptyDir.
- **Persistent, resume at level 1:** the PVC already exists, so there's no provisioning wait.
  The VM boots normally, and cloud-init doesn't redo first-boot work like making host keys. Not
  measured yet.
- **Persistent, resume at level 2:** the boot is replaced by a snapshot restore, which is
  expected to be well under a second. Pausing costs the time to write the memory file (1 GiB
  for a 1024 MiB VM). Neither is measured yet.

### Overhead of offering both

- **Code:**
  - a per-session setting in the LabSession spec
  - two paths in the controller: disk location, and whether to create `vm-data`
  - "create if missing" in `init.sh`
  - for level 2, a `Paused` phase plus pause and resume actions in the controller and API
- **Tests:** unit and e2e coverage for both modes, plus pause and resume.
- **Storage:** a persistent session keeps its writable disk (sparse, so it grows with use up to
  its size) until it's deleted. At level 2 it also keeps the memory file (the VM's memory size,
  1 GiB today) and a small state file. Paused sessions need a cleanup rule, such as deletion
  after N days.
- **Operations:**
  - Pinned to one node: local-path volumes live on one node, so a persistent session can only
    resume on the node that created it. If that node is full or down, it can't resume until the
    node is back. Moving sessions between nodes needs network storage.
  - Base image upgrades: the writable disk is a layer over one exact base image. A persistent
    session must keep booting the base version it started on, so old base versions are kept
    while sessions use them.
  - Snapshot compatibility: a level-2 snapshot also needs the same Firecracker version, CPU
    type, drive paths and tap device name on restore. If they change, the session falls back to
    a level-1 cold boot.

### Trade-offs

| | Non-persistent | Persistent |
|---|---|---|
| Startup | fastest: no PVC wait | first start as today; resume as fast as a boot (level 1) or faster (level 2) |
| Student experience | starts clean every time; work is lost when the pod goes | continues where they stopped |
| Storage cost | none after the pod ends | a disk per session (plus memory at level 2) until deleted |
| Scheduling | any node | resume only on the node holding the disk (local-path) |
| Upgrades | always the newest base | pinned to the base version it started on |
| Complexity | lowest | more code, tests and cleanup, especially at level 2 |

### Snapshots: disk level and VM level

A Firecracker VM has two kinds of state, and each has its own kind of snapshot. They are taken
separately and must be restored as a pair.

#### Disk-level snapshot: the files

- **What it captures:** the writable disk, meaning everything the session wrote to its
  filesystem (the student's files, installed packages, cloud-init's state). The read-only base
  isn't included; it's shared and never changes.
- **How it's taken:** copy the writable disk file while nothing writes to it, meaning the VM is
  paused or stopped. Firecracker only uses raw disk files (no qcow2), so the snapshot is a
  plain file copy, sparse to keep it small. A filesystem with reflinks (XFS, btrfs) makes the
  copy almost instant; ext4 copies the used blocks. Kubernetes `VolumeSnapshot` would work for
  the whole PVC, but the local-path provisioner doesn't support it.
- **Consistency:** if the VM is paused without flushing, recent writes may still be in the
  guest's memory rather than on the disk. That's fine when the disk is restored together with
  the matching memory snapshot. For a disk-only restore (a cold boot), run `sync` in the guest
  first, or the files may be missing their last writes.
- **Restore:** boot the VM normally on the saved disk (level 1). Files are back; running
  programs are not.
- **Size and time:** the size of the data the session wrote (sparse). Not measured yet.
- **Also useful for:** backups, save points a student can go back to, and cloning a session.

#### VM-level snapshot: the running machine

- **What it captures:** the VM's CPU and device state (a small state file) and its whole memory
  (the memory file, the size of the VM's memory: 1 GiB today). It does **not** include the
  disks. It records their paths and expects them unchanged on restore.
- **How it's taken:** pause the VM, then call Firecracker's `/snapshot/create`. A **Full**
  snapshot writes all of memory. A **Diff** snapshot writes only the pages changed since the
  last one, but needs dirty-page tracking turned on when the VM starts.
- **Restore:** start a new Firecracker process, call `/snapshot/load` with the state and memory
  files, then resume. Firecracker maps the memory file and loads pages as the guest touches
  them, so the restore itself is quick and the first moments after it are a little slower.
- **Requirements on restore:**
  - the same disk files at the same paths, in exactly the state they were in at snapshot time
  - the same Firecracker version, a compatible CPU type and the same tap device name
- **Inside the guest after a restore:**
  - The clock jumps by the time the session was paused, so the guest needs a time resync.
  - Network connections that were open when it paused are dropped, for example the terminal's
    SSH session, which reconnects.
  - If one snapshot is restored more than once (the faster-startup use), every copy has the same
    SSH host keys, random seed and cloud-init identity.

#### Pause and resume together (level 2)

On pause:
1. Run `sync` in the guest, so a disk-only fallback is also safe.
2. Pause the VM.
3. Take the VM snapshot: the state file and the memory file.
4. The writable disk is already on the PVC, and it's consistent because the VM is paused.
5. Delete the pod.

On resume:
1. A new pod mounts the same PVC.
2. `start-firecracker.sh` finds the snapshot files, loads them and resumes the VM.
3. The guest resyncs its clock, and the terminal reconnects.

If the restore is refused (a different Firecracker version or CPU), fall back to a level-1 cold
boot on the same disk.

| | Disk-level | VM-level |
|---|---|---|
| Captures | files on the writable disk | CPU, devices and memory (running programs) |
| Size | data written by the session | the VM's memory size (1 GiB today); smaller with Diff |
| Restore gives | a fresh boot with the files kept | the session exactly as it was |
| Works alone | yes | no: needs the matching disk |
| Survives upgrades | across Firecracker upgrades, yes (the base must stay the same) | needs the same Firecracker version and a compatible CPU |

#### What exists today in `start-firecracker.sh` (dozlab-infra)

- On start, if `SNAPSHOT_PATH` and `MEM_FILE_PATH` exist, it loads them and resumes instead of
  booting.
- On `SIGTERM`/`SIGINT` (pod deletion), if both are set, it pauses the VM, **deletes the previous
  snapshot**, takes a new Full snapshot and kills Firecracker.

Gaps to fix before relying on it:
- **It runs during pod shutdown.** Writing 1 GiB has to finish within the pod's termination
  grace period (30 s by default) or it's killed half-written.
- **It deletes the old snapshot first.** If the new one fails, the session has neither. Write to
  a temporary name and rename on success.
- **It never syncs the guest.** A cold-boot fallback may lose recent writes.
- **It doesn't check that the disks match.** A snapshot loaded against a different disk
  corrupts the session.
- **The image runs Firecracker v0.24.0 (2021)**, from before snapshots were stable (they were a
  developer preview until 1.0). `mem_file_path` is the right field for that version; newer
  versions use `mem_backend`. Upgrading Firecracker changes this code, and snapshots taken on
  the old version can't be restored on the new one. So upgrade before persistent sessions
  ship, not after.
- **Pausing is tied to deleting the pod.** A pause action should take the snapshot first, then
  delete the pod once it's written.

### Still to decide

- ~~Who picks the mode~~: for now, instructors do; see
  [Session options for end users](#session-options-for-end-users). The setting is
  `persistence`: `none` (default) or `files` (owner, 2026-09-30).
- How long paused sessions are kept before cleanup.
- Whether level 2 (snapshots) is needed, or level 1 is enough to start.

## Session options for end users

These are the options DozLab can offer for a lab VM. They're listed so we can decide later which
ones students actually need.

### Rollout (owner decision, 2026-09-30)

1. **Phase 1: instructors create VMs.** An instructor creates a lab VM and chooses its options
   from the list below. Students don't choose anything yet.
2. **Phase 2: what instructors give students.** Once instructors have used the options, decide
   which ones an instructor hands to students for a task (for example, "this assignment is
   persistent, with save points"), and whether students can choose any of them themselves.

### The options

| # | Option | What the user gets | What it costs them | Build status |
|---|---|---|---|---|
| 1 | **Non-persistent** (default) | a clean VM every start; work is gone when the VM stops | nothing extra; the fastest start | after option A is built |
| 2 | **Persistent: keep files** (level 1) | files are there on return; the VM boots again | a longer first start (storage volume); stays on one node and one base image; storage until cleanup | after option A plus the disk on `vm-data` |
| 3 | **Persistent: pause and resume** (level 2) | the VM comes back exactly as it was, with programs still running | everything in 2, plus a wait on pause (writes 1 GiB of memory) and more storage | needs the snapshot gaps fixed and a newer Firecracker |
| 4 | **Save points** | named copies of the disk to go back to | storage per save point; the VM pauses briefly while each is taken | needs 2, plus disk-level snapshots |
| 5 | **Start from a prepared VM** | an instructor sets up a VM (tools, files) once, and new VMs start from a copy of its disk | the copy step per new VM; the prepared disk is tied to one base image | needs 4 |
| 6 | **Fast start from a snapshot** | a VM that's ready in about a second instead of booting | every copy shares SSH host keys and identity until per-session setup moves after restore | needs 3, plus that setup change |

Options 1 and 2 are the first choice an instructor makes. Options 3–6 are extras on top of
persistence, apart from 6, which also fits non-persistent labs.

### Resources each option uses

An instructor should see what a VM will take before creating it. The numbers below are the
controller's defaults on `main` (`internal/controller/resource_builder.go`,
`pod_settings.go`). A lab can change CPU, memory, disk size and storage, so the UI should show
the lab's actual values. They're listed as resources rather than money so a price per unit can
be attached later.

**While a VM is running, every option uses the same CPU and memory.** Each VM is one pod with
three containers:

| Container | Reserved (request) | Maximum (limit) | What it's for |
|---|---|---|---|
| `firecracker-vm` | 1 CPU, 3 Gi | 2 CPU, 4 Gi | runs the VM; the VM itself gets 1 vCPU and 1024 MiB |
| `terminal-sidecar` | 250m CPU, 256 Mi | 500m CPU, 512 Mi | the browser terminal (SSH into the VM) |
| `code-server` | 500m CPU, 1 Gi | 1 CPU, 2 Gi | the browser editor |
| **Total per running VM** | **1.75 CPU, 4.25 Gi** | **3.5 CPU, 6.5 Gi** | plus one `/dev/kvm` and one `/dev/net/tun` from the node |

Reserved is what the cluster sets aside for the VM, so it's what limits how many VMs fit on a
node. The VM container reserves 3 Gi for a VM with 1 GiB of memory; lowering that is a separate
change that would fit more VMs on a node.

**The options differ in storage and in time.** Disk sizes use the defaults: 4 Gi writable disk
and the vm lab's base (362 MB with cloud-init; the k8s lab's is 1.4 GB).

| # | Option | Storage while running | Storage after the VM stops | Extra time |
|---|---|---|---|---|
| 1 | Non-persistent | base + writable disk up to 4 Gi, in node-local temporary space (limit 8 Gi) | **none** | none; fastest start |
| 2 | Persistent: keep files | base + writable disk up to 4 Gi on the `vm-data` volume | the writable disk (only what was written, up to 4 Gi) until cleanup | first start waits for the volume; each return is a full boot |
| 3 | Persistent: pause and resume | as 2 | as 2, **plus 1 GiB** of memory and a small state file per paused VM | pausing writes 1 GiB; resume is expected to be under a second |
| 4 | Save points | as 2 | as 2, plus one copy of the written data per save point | a short pause per save point |
| 5 | Start from a prepared VM | as 1 or 2 | one prepared disk per lab, shared by all its VMs | copying the prepared disk for each new VM |
| 6 | Fast start from a snapshot | as 1 or 2 | one snapshot per lab: its disk plus 1 GiB of memory | about a second to start instead of a boot |

**While a persistent VM is stopped or paused, it uses no CPU or memory**, only storage. Pausing
a VM a student isn't using frees 1.75 CPU and 4.25 Gi for others.

**Volumes today:** every session creates two volumes whatever the option: `vm-data` (10 Gi by
default, or the lab's `storage`) and `vscode-data` (5 Gi, the editor's settings). On the local
cluster's local-path storage these are directories on the node, and the size isn't enforced.
With the options in place, non-persistent VMs can skip `vm-data`, and a persistent VM's
`vm-data` holds its writable disk (and snapshot files at option 3).

### Keeping resources to a minimum

**Owner decision (2026-09-30):** be conservative. Every VM starts at the smallest baseline that
works, and only scales up for what a lab type's tools need. **The size is set on the lab**, in
its definition, by the instructor who creates it. VMs take the lab's size; nobody picks CPU,
memory or disk when creating a VM. Sizes change only with a measurement behind them.

#### Measured (2026-09-30)

Measured in Docker the way a lab pod runs (init container + `dozlab-firecracker`), with 1 vCPU
and a 4G disk. "Used" is inside the VM, 20 s after SSH came up, with no student activity.

| Lab | VM memory | Boot → SSH | Used in the VM | Result |
|---|---|---|---|---|
| vm | 1024 MiB | 5.5 s | 48 MiB | OK |
| vm | 512 MiB | 5.0 s | 47 MiB | OK, no failed units |
| vm | 256 MiB | 5.0 s | 46 MiB | boots, but cloud-init's locale step fails every time (`cloud-config.service`); generating the locale needs more memory |
| vm | 128 MiB | 5.5 s | — | SSH answers once, then the VM stops responding at ~48% CPU: too small |
| k8s | 2048 / 1024 / 512 MiB | 5.0–5.7 s | 70–73 MiB | boots; containerd runs. Not representative: Kubernetes isn't running (see [Why the k8s lab doesn't run Kubernetes](#why-the-k8s-lab-doesnt-run-kubernetes)) |

Other parts, idle:
- `terminal-sidecar`: 10.7 MiB, 0% CPU (reserves 256 Mi today)
- the controller: 9–12 MiB and 1–2m CPU per replica (reserves 256 Mi and 250m each, ×3 replicas)
- `code-server`: not measured (the image isn't on this machine)

**Less memory doesn't make the VM start slower.** Boot → SSH was about 5 s at every size. On the
host, the Firecracker container counted 140–560 MiB. That figure includes the kernel's cache of
the disk file, so it's higher than the VM's own use. The VM can never use more than its memory
size plus the Firecracker process itself (not yet measured on its own).

#### Where resources go today and aren't used

| What | Reserved today | Used / needed | Note |
|---|---|---|---|
| VM container | 1 CPU, 3 Gi | VM of 1 vCPU and 1024 MiB; 47 MiB used idle | the lab's `resources` only change the container, not the VM: the VM is fixed at 1 vCPU / 1024 MiB (`vmCPUCount`, `vmMemoryMiB` in `resource_builder.go`) |
| Terminal sidecar | 250m, 256 Mi | 10.7 MiB idle | |
| code-server | 500m, 1 Gi | not measured | on every VM, even when a lab doesn't use the editor |
| Per-VM volumes | `vm-data` 10 Gi + `vscode-data` 5 Gi | vm lab base 372 MB; a session writes what it writes | created for every VM, even non-persistent |
| Temporary disk | limit 8 Gi (twice the 4 Gi disk) | | with the writable disk, base + writable is enough |
| Controller | 3 replicas × 250m, 256 Mi | 1–2m, ~10 MiB each | only one replica works at a time (leader election) |
| RabbitMQ | | | crash-looping (105 restarts in 8 h): its Erlang cookie file is readable by others (`/var/lib/rabbitmq/.erlang.cookie must be accessible by owner only`). Each restart costs CPU |

#### The barest minimum (proposed, for the vm lab)

Proposed baseline, to be confirmed under load (a student compiling, running tools) before it
becomes the default:

| Part | Request | Limit | Why |
|---|---|---|---|
| VM | 1 vCPU, **512 MiB** | | smallest size with no failures; 256 MiB may work once the locale is built into the image instead of generated at first boot |
| VM container | 100m CPU, VM memory + Firecracker overhead (overhead to be measured) | 1 CPU, same memory | idle CPU was 0.2–2.5%; the limit lets it use its whole vCPU when busy |
| Terminal sidecar | 10m CPU, 32 Mi | 100m, 64 Mi | 10.7 MiB idle; check with a few open terminals |
| code-server | off unless the lab needs it | | a lab-level choice |
| Writable disk | 1 Gi | | the base holds the OS; the writable disk only holds changes |
| Volumes | none for non-persistent; `vm-data` sized to the writable disk for persistent | | `vscode-data` only when code-server is on |
| Controller | 1 replica | | 3 are only needed for failover |

What that means on this node (8 CPU, ~19.2 GiB, 20 KVM devices; 1.1 CPU and 1.15 GiB already
reserved by the platform):

| Per VM | Reserved per VM | VMs that fit | Limited by |
|---|---|---|---|
| Today | 1.75 CPU, 4.25 Gi | **3** | CPU |
| Proposed minimum, no editor | ~0.11 CPU, ~0.6 Gi (+ Firecracker overhead) | **20** | KVM devices (20); memory would allow ~30 |
| Proposed minimum + code-server as today | ~0.61 CPU, ~1.6 Gi | **11** | CPU and memory |

Low CPU requests mean VMs share CPU when many are busy at once. They stay capped at their vCPU
count, so a busy VM can't take more than its share, but a full class compiling together will be
slower. That is the trade for fitting more VMs.

#### Scaling up per lab type

The lab's tools decide its size. Start from the baseline and add what the tools need. Measure
each lab type before setting its size.

| Lab type | Starting size | Status |
|---|---|---|
| vm (Linux, shell tools) | 1 vCPU, 512 MiB, 1 Gi writable | measured idle; confirm under load |
| custom-initrd (tiny Alpine, 41 MB rootfs) | 1 vCPU, probably 128–256 MiB | not measured |
| k8s | 2 vCPU, 2 GiB, 4 Gi writable: kubeadm's documented minimum for a control-plane node | idle measured only; Kubernetes doesn't run yet (below) |
| any lab + editor | add code-server | measure code-server |

At what scale this matters: **from the first VM.** Today one VM reserves 4.25 Gi, so this node
holds 3. The platform's own fixed costs (controller replicas, RabbitMQ) matter most on a small
cluster like this one; per-VM sizes matter more as the number of VMs grows.

#### Why the k8s lab doesn't run Kubernetes

Diagnosed 2026-09-30 by booting the k8s lab (2 vCPU, 2048 MiB; Kubernetes v1.30.14; guest kernel
4.14.174). The causes, in the order they'd be hit:

1. **Nothing sets up the node.** No one runs `kubeadm init`, so the kubelet's config
   (`/var/lib/kubelet/config.yaml`) never exists and the kubelet restarts forever.
2. **An outdated kubelet flag.** The kubelet's systemd drop-in passes `--container-runtime=remote`,
   which was removed in Kubernetes 1.27. Kubelet 1.30 will refuse to start even with a config.
3. **The guest kernel is too old for cgroup v2.** The VM uses cgroup v2, but kernel 4.14 offers
   only the `io`, `memory` and `pids` controllers there, with no `cpu` or `cpuset`. Kubernetes
   documents kernel 5.8 or later for cgroup v2, so the kubelet is expected to fail its cgroup
   checks, and CPU limits can't be enforced. **Owner chose a newer kernel (2026-09-30):**
   dozlab-infra #8 moves to Firecracker's CI build of 5.10.245, which has the `cpu` and `cpuset`
   controllers and boots on Firecracker v0.24 (the 6.1 build doesn't: it needs a newer
   Firecracker). Tested: vm lab SSH in 4.5 s at 512 MiB, all controllers present.
4. **Found after the kernel change: kube-proxy's iptables features.** Firecracker's CI kernels
   (5.10 and 6.1) are built without `xt_comment`, `xt_statistic`, `xt_mark`, `xt_multiport` and
   nf_tables, and without module support. kube-proxy uses these for Services, so a working k8s
   lab needs a custom kernel build with them turned on. Where that's built is still open.

**Not a cause:** `systemd-modules-load` fails on `br_netfilter`, but only because the image has no
module files for 4.14 (it has `/lib/modules/5.15.0-194-generic` from an unused Ubuntu kernel).
`br_netfilter` and `overlay` are built into the kernel and work (`bridge-nf-call-iptables=1`).
Earlier notes said `br_netfilter` was missing; that was wrong.

The k8s lab's size can only be measured once these are fixed. Until then, the starting size is
kubeadm's documented minimum for a control-plane node: 2 CPU and 2 GiB.

#### Which options use more and which use less

While running, every option reserves the same CPU and memory (the lab's size). They differ in
storage, from least to most:

1. **Non-persistent:** nothing after the VM stops.
2. **Start from a prepared VM:** one prepared disk per lab, shared.
3. **Fast start from a snapshot:** one snapshot per lab (its disk + the VM's memory).
4. **Persistent, keep files:** each VM's written data until cleanup.
5. **Save points:** 4, plus one copy of the written data per save point.
6. **Pause and resume:** 4, plus **the VM's memory size** per paused VM. A 512 MiB VM means a
   512 MiB snapshot, so a smaller VM also makes pausing faster and cheaper.

A stopped or paused persistent VM reserves no CPU or memory, so pausing idle VMs frees room for
others.

#### Estimating what a lab needs

For a lab with VM size `cpu_req`, `mem_req` (from the lab), writable data per VM `w`, and VM
memory `m`:

- **CPU and memory** = VMs running at the same time × (`cpu_req`, `mem_req`), plus the platform
  (about 1.1 CPU and 1.15 GiB on this cluster today)
- **KVM devices** = VMs running at the same time (20 per node here)
- **Storage** = running non-persistent VMs × `w` (temporary)
  + persistent VMs × `w`
  + paused VMs × `m`
  + save points × `w`
  + per lab: the base image once per node (372 MB for vm), plus a prepared disk or snapshot if used

Example: a vm lab for 30 students, persistent (keep files), 10 working at once, ~200 MB written
each (an assumption, not measured):
- today's sizes: 10 × 4.25 Gi = 42.5 Gi of memory reserved; this node holds 3, so 4 nodes
- proposed minimum: 10 × ~0.6 Gi = ~6 Gi; fits on this node
- storage: 30 × 200 MB = ~6 GB kept until cleanup

The API's estimate (phase 1) uses the same formula with the lab's own sizes.

### What phase 1 needs in the API

- **A role check.** The `instructor` role exists (`internal/models/models.go`, one of `admin`,
  `instructor`, `student`), but no route checks it today: any logged-in user, including
  students, can call `POST /api/v1/labs` and create sessions. Only `/api/v1/admin` routes check
  a role. Phase 1 lets `instructor` and `admin` create VMs with options, and keeps students
  from setting them. **Only instructors and admins create labs** (owner, 2026-09-30):
  `POST /api/v1/labs` gets the same role check.
- **The options on VM creation.** `CreateLabSessionRequest` (`internal/api/handlers/lab_session.go`)
  already takes `resources` and `config`. The option goes there as `config.persistence`
  (`none`, the default, or `files`; owner, 2026-09-30), and down to the LabSession spec in the
  controller. The other options get values when they're built.
- **Clear wording in the UI.** When an instructor picks a persistent option, show the costs from
  [the notice above](#persistent-and-non-persistent-sessions) before the VM is created.
- **Resources before and after creation.** Before creating a VM, show the instructor what it
  will reserve and store (the tables in [Resources each option uses](#resources-each-option-uses),
  filled in with the lab's own CPU, memory, disk and storage). After that, show what each of
  their VMs and labs is using: running or stopped, the CPU and memory it holds while running,
  and the storage it keeps (writable disk, snapshots, save points). Nothing reports this today;
  the storage figures would come from the volumes and snapshot files, and the CPU and memory
  from the pod.

### Decided in phase 2

- Which options instructors give students, per lab or per assignment.
- Whether students can pick any option themselves (for example, turning on persistence).
- Limits per student: how many persistent VMs and save points, and how much storage.

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

---

# Decision: Firecracker and guest kernel versions

- **Status:** accepted (2026-09-30)
- **Decision:** option B, Firecracker **v1.15.1** with guest kernel **6.1.155**, both from the
  same Firecracker CI release set (dozlab-infra #9). A custom 6.1 kernel for the k8s lab comes
  later (D).

## In short

The VM runs on two pieces that have to be chosen together:

- **Firecracker**, the program that runs the VM (in the `dozlab-firecracker` image). Today:
  v0.24.0, from 2021.
- **The guest kernel**, the Linux kernel inside the VM (`/find/vmlinux.bin` in the same image).
  Today: 5.10.245.

They depend on each other: a kernel only boots if it understands how that Firecracker version
describes the VM's devices. Firecracker publishes the kernels it tests each release line with,
so the safest pairing is a Firecracker version and a kernel from the same published set.

## What we found (2026-09-30)

| Firecracker | Kernel | Result |
|---|---|---|
| v0.24.0 | 4.14 (the old kernel) | boots; no `cpu` or `cpuset` cgroup controllers, so Kubernetes can't run and CPU limits can't be enforced |
| v0.24.0 | 5.10.245 (CI v1.15) | **boots** (vm lab SSH in 4.5 s at 512 MiB); all cgroup controllers present. Now on `main` (dozlab-infra #8) |
| v0.24.0 | 6.1.155 (CI v1.15) | **kernel panic**: `VFS: Cannot open root device "vda"` |
| v1.17.0 | 5.10.245 and 6.1.155 | boot test run; results not reviewed (not chosen) |
| **v1.15.1** | **6.1.155 (CI v1.15)** | **boots.** vm lab 512 MiB: SSH in 4.9 s, 2 vCPUs, 482 MiB visible, all cgroup controllers, no failed units. k8s lab 2 GiB: SSH in 4.4 s; the kubelet waits for `kubeadm init` |
| v1.15.1 | 6.1.155, snapshot | stop took 8.8 s (512 MiB memory + 14 KB state); restart resumed from the snapshot, SSH in **1.8 s**; a file and a running process (same PID) survived |

**Why 6.1 panics on v0.24:** Firecracker v0.24 announces the VM's disks and network card with a
`virtio_mmio.device=` boot option. Newer Firecracker versions describe devices through ACPI
tables instead. The 5.10 CI build still reads the boot option
(`CONFIG_VIRTIO_MMIO_CMDLINE_DEVICES=y`); the 6.1 CI build doesn't, and v0.24 has no ACPI, so 6.1
finds no disk. **So 6.1 needs a newer Firecracker.**

**What Firecracker's CI publishes:** guest kernels per release line in its public bucket under
`firecracker-ci/vX.Y/`. The newest sets are `v1.14/` and `v1.15/`; v1.15 has 5.10.245 and
6.1.155. Firecracker v1.16.x and v1.17.0 (released 2026-09-10) have no published kernel set, so
**v1.15.1 (2026-04-07) is the newest Firecracker published together with its tested kernels.**

**What the kernel still lacks for the k8s lab:** all of Firecracker's CI kernels (5.10 and 6.1)
are built without `xt_comment`, `xt_statistic`, `xt_mark`, `xt_multiport` and nf_tables, and
without module support. kube-proxy needs these for Kubernetes Services, so a working k8s lab
needs a custom kernel build whichever option is chosen (see
[Why the k8s lab doesn't run Kubernetes](#why-the-k8s-lab-doesnt-run-kubernetes)).

## Options

### A. Stay on Firecracker v0.24.0 with kernel 5.10 (today)

| Pros | Cons |
|---|---|
| Works now for the vm lab; nothing more to change | Kernel 5.10 reaches end of life in December 2026 |
| | v0.24 can't boot 6.1, so the kernel can't move forward |
| | Snapshots were a developer preview in v0.24; persistent sessions would be built on it and then broken by the upgrade (snapshots don't restore across versions) |
| | Five years of Firecracker fixes missing |

### B. Firecracker v1.15.1 with kernel 6.1.155 (recommended)

| Pros | Cons |
|---|---|
| Firecracker and kernel come from the same published CI set | Not the newest Firecracker (v1.17.0 is) |
| Kernel 6.1 is supported for longer than 5.10 | Needs the API changes below |
| Stable snapshots, needed before persistent sessions ship | Still needs a custom kernel for the k8s lab |
| Kernel 5.10.245 from the same set stays available as a fallback | |

### C. Firecracker v1.17.0 with kernel 6.1.155

| Pros | Cons |
|---|---|
| Newest Firecracker | No kernel set is published for v1.17, so this pairing isn't one Firecracker published |
| Same API changes as B | If something breaks, it's unclear whether Firecracker or the kernel is at fault |

### D. Our own kernel build (needed for the k8s lab in any option)

Build 6.1 from Firecracker's CI config for v1.15, plus the iptables and nf_tables options
kube-proxy needs. **Done in dozlab-infra #10** (owner chose dozlab-infra, 2026-09-30): a
Dockerfile stage builds 6.1.155 from kernel.org with `kernel/firecracker-6.1.155.config` plus
`kernel/dozlab.config`, and fails if an option is dropped. A cold build takes about 10 min
(7.7 min compiling on 8 cores); Docker caches it afterwards.

Tested with the k8s lab (dozlab-rootfs-manager #10, first-boot `kubeadm init`) at 2 vCPUs and
2 GiB: `kubeadm init` done 64 s after VM start, node Ready, all 7 kube-system pods Running
(kube-proxy in iptables mode on nf_tables), Services and DNS work from pods, 493–509 MiB used.
In-pod DNS is ready about 95 s after boot.

## What upgrading Firecracker changes (B or C)

Done in dozlab-infra #9:

- **The release tarball layout:** the binary is now at
  `release-<version>-x86_64/firecracker-<version>-x86_64`. The Dockerfile pins the version and
  the release's published SHA-256.
- **`machine-config`:** `ht_enabled` was renamed `smt` in Firecracker 1.0.
  `start-firecracker.sh` ignores API errors, so on 1.x the old field would be rejected silently
  and the VM would start with Firecracker's defaults (1 vCPU, 128 MiB). The boot tests check the
  VM's vCPUs and memory from inside to catch this.
- **Snapshot loading:** `mem_file_path` is replaced by `mem_backend` (`backend_type: File`,
  `backend_path`).
- **Snapshots:** taken on one Firecracker version can't be restored on another. Upgrade before
  persistent sessions ship, not after.

## Still to decide

- ~~Which option~~: B (owner, 2026-09-30).
- ~~Where the custom kernel for the k8s lab is built (D)~~: dozlab-infra (#10).
- ~~Whether to keep 5.10.245 as a fallback~~: documented in the Dockerfile as build arguments,
  not shipped in the image.

# Decision: frontend on GitHub Pages, backend through a tunnel, code-server through Traefik

- **Status:** accepted (2026-09-30, owner)
- **Decision:** the Nuxt frontend is a static site on GitHub Pages at
  `https://dozlab.github.io/dozlab-frontend/`. The browser calls the backend on the owner's machine
  through Tailscale Funnel at `https://dozmanlab.taildc994d.ts.net`. Funnel sends everything to the
  cluster's Traefik, which routes `/api` to dozlab-api and each session's code-server and terminal
  by path. The controller creates one Ingress per session.
- **Security is out of scope for now (owner decision).** The routes below have no auth in front of
  them beyond what each service already does. See "Security later".

## Context

The owner doesn't want to pay for a domain or DNS. The question was whether VS Code Server
(code-server) can run on GitHub Pages.

**It can't.** Pages only serves static files (HTML, JS, CSS). It can't run code-server, the Go API
or anything else. What can live on Pages is the **frontend**. The browser then talks straight to
the backend, which is where code-server already runs (the `code-server` container, port 8080, in
every lab pod: `dozlab-controller/internal/controller/resource_builder.go`).

Constraints that follow:

- **The backend needs a public HTTPS URL.** Pages is served over HTTPS, and browsers block `http://`
  and `ws://` calls from an HTTPS page (mixed content). A home IP over plain HTTP won't work.
- **The API must allow the Pages origin (CORS).** Before this change it sent no CORS headers.
- **The frontend must call the API at an absolute URL.** Its stores called relative paths such as
  `/api/labs`, which on Pages would go to github.io.

```
Browser ── https://dozlab.github.io/dozlab-frontend/   (static Nuxt build, GitHub Pages)
   │
   └── https / wss ──> https://dozmanlab.taildc994d.ts.net   (Tailscale Funnel, on the node)
                          │
                          └──> Traefik (k3s built-in, kube-system, node ports 80/443)
                                 ├─ /api/*                     → dozlab-api
                                 ├─ /sessions/<id>/vscode/*    → lab-service-<id>:8080  (prefix stripped)
                                 └─ /sessions/<id>/terminal/*  → lab-service-<id>:8081  (prefix stripped)
```

## Where the frontend lives

DozLab is a GitHub **organization**, so it gets `dozlab.github.io` free, with no domain or DNS.

| Option | URL | What it takes |
|---|---|---|
| **A. Deploy from `dozlab-frontend` (chosen)** | `https://dozlab.github.io/dozlab-frontend/` | Pages on the existing repo plus an Actions workflow. Nuxt needs `app.baseURL: '/dozlab-frontend/'` because the site sits under a sub-path |
| B. New repo `DozLab/dozlab.github.io` | `https://dozlab.github.io/` | Cleaner URL, but code in one repo and the build pushed to another: one more token and moving part |

A: one repo, one workflow, nothing to sync.

## The tunnel: a public HTTPS URL without a domain

| Option | URL | Notes |
|---|---|---|
| **Tailscale Funnel (chosen)** | `https://dozmanlab.taildc994d.ts.net` (stable) | Free with an account; the node is already logged in to the tailnet. The URL doesn't change, so the frontend doesn't need rebuilding |
| Cloudflare quick tunnel | `https://<random-words>.trycloudflare.com` | No account, but the URL changes on every restart, so the frontend's API URL goes stale |
| ngrok | one free static domain | Another account and agent |

Funnel only needs one target: Traefik on the node (`http://192.168.1.91:80`). Traefik does the
routing, so the tunnel never changes when services are added.

## Who proxies code-server: Traefik or dozlab-api

Traefik is already running: it's the copy k3s installs itself (`kube-system/traefik`, LoadBalancer on
`192.168.1.91` ports 80 and 443). Until this change it routed nothing (no Ingress or IngressRoute in
the cluster). The Helm chart in `~/traefik` (chart 30.1.0, Traefik v3.1.2, downloaded 2024-08) is a
stock copy, is not what's running, and isn't needed.

### Chosen: Traefik, with the controller creating a per-session Ingress

1. **It keeps the API out of the editor traffic.** Traffic splits into two kinds:
   - **Control traffic** (API): short requests like login, "start a lab" or "list sessions".
   - **Data traffic** (code-server and terminal): long-lived WebSockets carrying every keystroke,
     file save and terminal byte.

   If the API proxies code-server, it's in the path of all the data traffic: restarting or
   redeploying the API drops every open editor, and heavy editor traffic competes with logins.
   Traefik exists for this job and already runs.
2. **The controller already follows this pattern.** For each `LabSession` it creates a PVC, a Pod
   and a Service, each tied to the session with `SetControllerReference`. The Ingress is a fourth
   resource built the same way: Kubernetes deletes it when the session is deleted, so there's no
   cleanup code, and the route exists exactly as long as the lab does. With the API as proxy, the
   API would have to look up pod addresses and handle ended sessions itself, which is state the
   controller already manages.
3. **Security can be added later without a redesign.** The API knows who owns which session, which is
   the best argument for proxying there. Traefik's `forwardAuth` middleware can ask the API "may
   this user open session X?" before letting a request through: the API stays the decision-maker
   and Traefik still carries the traffic. This control plane / data plane split is common
   (JupyterHub, for example, runs a separate proxy in front of each user's server).

### Not chosen: dozlab-api as a reverse proxy

One Go file (`httputil.ReverseProxy` handles WebSockets) and no Kubernetes changes. Quicker to a
demo, but it's the choice most likely to be undone later, for the reasons above.

## How it's built

**Why the prefix is stripped.** code-server and the terminal sidecar serve from `/`. code-server
uses relative URLs, so it works under any prefix as long as the proxy strips it before
forwarding. Traefik does that with a `StripPrefixRegex` Middleware (`^/sessions/[^/]+/[^/]+`). The
Middleware is a Traefik CRD, so the controller doesn't create it: the Helm chart creates it once in
the labs namespace, and the controller only references it by annotation. Traefik only allows a
Middleware from the Ingress's own namespace (k3s leaves `allowCrossNamespace` off), so it must live
in the namespace the sessions run in. The URL must keep its trailing slash
(`/sessions/<id>/vscode/`); without it code-server's relative paths resolve one level too high.

| Repo | Change |
|---|---|
| dozlab-controller | `BuildIngress`: `lab-ingress-<id>`, two paths (`/sessions/<id>/vscode`, `/sessions/<id>/terminal`) to the session Service; class from `INGRESS_CLASS` (default `traefik`), Middleware annotation from `INGRESS_MIDDLEWARE`. `ensureIngress` after the Service, owner reference set, `Owns(&networkingv1.Ingress{})`. Status endpoints become `<PUBLIC_BASE_URL>/sessions/<id>/vscode/` and `.../terminal/` (a relative path when `PUBLIC_BASE_URL` is unset). RBAC for `networking.k8s.io/ingresses` |
| dozlab-api | CORS middleware; allowed origins from `CORS_ALLOWED_ORIGINS` (comma-separated), default `https://dozlab.github.io,http://localhost:3000` |
| dozlab-infra (Helm chart) | API Ingress by path (`/api`, host optional); the strip-prefix Middleware in the labs namespace; controller env `INGRESS_CLASS`, `INGRESS_MIDDLEWARE`, `PUBLIC_BASE_URL`; controller RBAC for Ingresses |
| dozlab-frontend | Static build (`ssr: false`, `nuxt generate`), `app.baseURL` from `NUXT_APP_BASE_URL`, one `$fetch` client on `apiBase` with the JWT, store paths fixed to the API's `/api/v1/...` routes, an "Open VS Code" link from the session's endpoints, and a GitHub Actions workflow that deploys to Pages. `API_BASE_URL` is a repo variable |

Tunnel, run once on the node (the Funnel config survives reboots):

```
tailscale funnel --bg http://192.168.1.91:80
```

**The editor opens in a new tab, not an iframe.** code-server's password login sets a cookie. Inside
an iframe on `dozlab.github.io`, that cookie belongs to another site (`ts.net`), and browsers
block third-party cookies, so the login would loop. In its own tab, the cookie is first-party and
the password login works. An iframe becomes possible once auth moves to Traefik `forwardAuth` and
code-server runs with `--auth none`.

## Consequences and follow-ups

- **Anyone with the Funnel URL reaches the API and every session's routes.** code-server still asks
  for the session password; the terminal sidecar has no auth of its own.
- **The per-session Service is still `type: LoadBalancer`.** On k3s, each one starts a `svclb` pod
  that claims host ports 8080, 8081 and 22, so a second session's svclb can't be scheduled. With the
  Ingress, the Service can be `ClusterIP`. Not changed here; left for its own change.
- **The API isn't deployed in the local cluster yet** (only the controller is, and RabbitMQ is
  crash-looping), so the `/api` route has no backend until it is.
- **The frontend is an early skeleton.** Its response types (for example, login returns `tokens`,
  not `token`) and several pages don't match the API yet. This change only fixes the paths it calls
  and how it reaches the API.

## Security later

In order: code-server with `--auth none` behind Traefik `forwardAuth` to a new API endpoint that
checks the JWT and session ownership (then iframes work too); the same for the terminal route;
`CheckOrigin` on `/api/v1/ws` limited to the CORS origins; rate limits on `/api/v1/auth/*`.
