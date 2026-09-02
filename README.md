<div align="center">

# ⚡ GoDrop

AirDrop-style file sharing for your terminal. One machine hosts a room,
everyone else joins with a room key, and files (or whole folders) get sent
to the room over the local network.

[![CI](https://github.com/zuhailz/GoDrop/actions/workflows/ci.yml/badge.svg)](https://github.com/zuhailz/GoDrop/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

</div>

I kept needing to move files between my laptop and my desktop and got tired
of USB sticks and upload links, so this happened. Run `godrop host` on one
machine: it starts a room and prints a room key like
`4F8A2C61-B0D3E79A-15C6F2B8-9E3D4A07`. That string is the room's address and
its password in one. Press `c` to copy it, then on another machine run
`godrop connect <key>`. The receiver finds the host over mDNS, both sides
prove they know the key, and the dashboards light up. Press `/` on the host
to offer a file or a folder, and it lands on every connected receiver at
once.

No accounts, no server to deploy, no cloud in the middle, no size limit.
Every message is encrypted end to end and nothing leaves your network.

```text
┌─ HOST ─────────────────────────────┐   ┌─ RECEIVER ──────────────────────┐
│ CONNECTED PEERS                    │   │ PENDING OFFERS                  │
│  ▸ alice                           │   │  ▸ holiday-photos.zip   148 MB  │
│  ▸ bob                             │   │                                 │
│                                    │   │ ACTIVE TRANSFERS                │
│ TRANSFERS                          │   │ holiday-photos.zip              │
│ holiday-photos.zip → alice         │   │ ██████████████████░░  89%       │
│ ████████████░░░░░░░░  61%          │   │                                 │
│                                    │   │ FEED                            │
│ FEED                               │   │  ✓ received holiday-photos.zip  │
│  • suhail offered holiday-photos…  │   └─────────────────────────────────┘
│  ✓ alice accepted                  │
│  ✓ bob received                    │
└────────────────────────────────────┘
```

## Why another file transfer tool

- Private by default: AES-256-GCM on every message, plus mutual room-key
  authentication on both sides of the connection. There is no relay to trust.
- Zero setup: a single static binary per machine, installed with `go install`.
- One to many: offer a file once and every peer in the room gets offered a
  copy at the same time.
- Folders, not just files: a directory is zipped on the way out and restored
  as a real directory on the other side. Symlinks are skipped.
- Auto discovery: receivers find the host over mDNS using a name derived
  from the room key. Direct IP mode is there when multicast is blocked.
- You approve everything: nothing touches disk until you press accept, and
  name collisions get renamed (`report(1).pdf`), never overwritten.
- A terminal UI that's actually pleasant: live progress bars, peer lists,
  and one shared event feed on every screen.

## Install

Needs [Go](https://go.dev) 1.26 or newer.

```bash
go install github.com/zuhailz/GoDrop/cmd/godrop@latest
```

That's the whole distribution story: your machine compiles the pinned source
from the module proxy, so there are no release artifacts to download or
trust.

## Walkthrough

Two machines on the same Wi-Fi or LAN. Terminal 1 is the host, terminal 2
the receiver.

### 1. Start a room (host)

```console
$ godrop host --name suhail
```

A short splash plays, then you get the host dashboard and a fresh 128-bit
room key. It changes every time you start a room.

### 2. Join the room (receiver)

```console
$ godrop connect 4F8A2C61-B0D3E79A-15C6F2B8-9E3D4A07 --name alice
```

Discovery is mDNS, so as long as both machines are on the same network the
receiver finds the host on its own. Both dashboards then show each other
under CONNECTED PEERS, and the feed logs `alice connected`.

If your network blocks multicast (guest and hotel Wi-Fi love to do this),
dial the host directly instead:

```console
$ godrop connect 4F8A2C61-B0D3E79A-15C6F2B8-9E3D4A07 --ip 192.168.1.20:7777
```

### 3. Send something

Press `/` on the host to open command mode:

- `/send ~/Documents/report.pdf` offers a single file
- `/send ~/Pictures/holiday` offers a whole folder
- `/send` with no argument opens the built-in file browser: arrows move,
  `enter` opens a directory, `s` sends the highlighted item, `backspace`
  goes up, `esc` closes

### 4. Receive

The receiver lists incoming offers under PENDING OFFERS. `↑`/`↓` selects,
`a` or `enter` accepts, `r` rejects. Accepted files land in `--save-dir`
(the current directory by default). If `report.pdf` already exists you get
`report(1).pdf` and a note in the feed.

### 5. Watch it finish

Both screens share one feed: offered, accepted, received. Chunks are
CRC32-checked as they arrive and written to a `.part` file; only a complete,
verified file gets renamed to its real name. When the bar is full, the file
is done. `q` quits the receiver, `/exit` closes the room.

## How it works

The handshake, in order:

1. The host advertises `_godrop._tcp` over mDNS. The instance name is
   `godrop-` plus 16 hex chars of `HMAC-SHA256(room key)`, so a room can't
   even be found without the key.
2. Both sides exchange ECDH P-256 public keys and run the shared secret
   through HKDF-SHA256 to get the same 256-bit AES-GCM key.
3. The host sends a PinChallenge. The receiver answers with an HMAC tag
   that only someone holding the room key can compute, then turns around
   and verifies the host's tag the same way. Wrong key (or an impostor
   host) and the connection is dropped.
4. The receiver sends a PeerJoin with its ID and display name.
5. The host offers files; the receiver accepts or rejects each one.
6. Accepted transfers stream as 32 KiB chunks inside encrypted envelopes.
   Each chunk carries a CRC32 checksum and an offset, which the receiver
   clamps to the advertised file size before writing.
7. Joins, leaves, offers, accepts and completions all show up in a shared
   feed on every screen.

### Wire format

Every frame is a 1-byte message type, a 4-byte big-endian length, and a
JSON payload. Control messages travel inside EncryptedPacket envelopes so
only the two endpoints can read them.

| Type | Message | Direction | Carries |
|---:|---|---|---|
| 0 | KeyExchange | both | ECDH P-256 public key |
| 1 | FileOffer | host → peers | transfer ID, filename, size, folder flag |
| 2 | FileAccept | peer → host | transfer ID |
| 3 | FileReject | peer → host | transfer ID |
| 4 | Chunk | host → peer | offset, 32 KiB payload, CRC32 |
| 5 | PeerJoin | peer → host | peer ID, display name |
| 7 | EncryptedPacket | both | encrypted inner message |
| 9 | SystemEvent | host → all | shared feed entries |
| 10 | PinResponse | both | HMAC tag |
| 11 | PinChallenge | host → peer | starts the auth exchange |

Types 6 (PeerLeave) and 8 (Progress) are reserved for future use.

### Layout and concurrency

```text
cmd/godrop/          CLI entry point (cobra)
internal/
├── banner/          splash art
├── crypto/          ECDH, HKDF, AES-GCM, room key, auth tags
├── discovery/       mDNS advertise and resolve
├── host/            room host: peers, offers, fan-out, folder zip
├── protocol/        frame encoding and message types
├── receiver/        receiver: auth, chunk writing, extraction, renames
├── transfer/        transfer state, chunk reader, file writer
└── tui/             Bubble Tea apps, styles, shared components
```

The topology is hub and spoke: the host owns the room, and every accepted
peer gets its own goroutine streaming chunks independently. A per-peer
write mutex keeps frames from interleaving. Progress and feed events reach
the UIs over buffered channels, so the UI only redraws when something
happens.

## Security model

What it does:

- AES-256-GCM on every control message and every chunk, with a fresh
  random nonce per message.
- Mutual room-key authentication, bound to the key exchange and to each
  side's role. Wrong-key receivers can't join, and a rogue host answering
  a direct-IP dial can't pass itself off as yours.
- Keyed discovery: rooms can't be found on the network without the key.
- CRC32 integrity per chunk, plus offsets clamped to the advertised size.
- Remote filenames sanitized before they touch a path; extraction is
  guarded against zip-slip.
- `.part` files plus atomic rename, so you never see half a file under its
  real name.

What it doesn't do (yet):

- Transfer resume. An interrupted transfer leaves a `.part` file behind;
  delete it and start over.
- Internet transfers. LAN only, there is no relay.
- Anything about traffic analysis. Packet sizes and timing leak metadata.
- Symlinks inside offered folders are skipped rather than followed.

The whole design hangs on the room key. Both sides prove they know it
every session, so passive sniffers and uninvited peers fail closed. Paste
it only to people you trust, and restarting a room issues a fresh one.

## CLI reference

```text
godrop host [--name <name>]
godrop connect <room-key> [--name <name>] [--ip <ip:port>] [--save-dir <dir>]
godrop --version
```

| Flag | Command | Default | Description |
|---|---|---|---|
| `--name`, `-n` | both | random | Display name other peers see |
| `--ip`, `-i` | connect | | Dial this address directly, skips mDNS |
| `--save-dir`, `-s` | connect | `.` | Where received files and folders are written |

Default TCP port: 7777.

## Keybindings

Host, press `/` to enter command mode:

| Key | Action |
|---|---|
| `/send <path>` | offer a file or folder |
| `/send` | open the file browser |
| `/peers`, `/help` | list connected peers, show help |
| `/exit`, `/quit` | shut the room down |
| `[` / `]` | scroll the feed |
| `c` | copy the room key |

Receiver:

| Key | Action |
|---|---|
| `↑` / `↓` | highlight a pending offer |
| `a` / `enter` | accept the selected offer |
| `r` | reject it |
| `[` / `]` | scroll the feed |
| `c` | copy the room key |
| `q` | quit |

## Troubleshooting

**Room key copy does nothing on Linux.**
On Linux the copy goes through `wl-copy` (Wayland) or `xclip`/`xsel` (X11),
and a fresh Ubuntu has neither preinstalled:

```bash
sudo apt install xclip         # X11 sessions
sudo apt install wl-clipboard  # Wayland sessions
```

Without one of them, GoDrop falls back to the OSC 52 escape sequence, which
some terminals honor and others -- including Ubuntu's default GNOME Terminal,
historically -- ignore. The feed tells you when a copy could not be
confirmed, and the room key is always shown inline on screen, so selecting
and copying it manually always works.

**Receiver says "host not found".**
mDNS needs multicast, and guest Wi-Fi, AP isolation, hotel and corporate
networks commonly block it. Put both machines on the same ordinary network,
or skip discovery with `godrop connect <room-key> --ip <host-ip>:7777`.

**Windows can't discover anything.**
mDNS on Windows relies on Bonjour, which comes with iTunes. Install it, or
connect with `--ip`.

**Connection refused or times out.**
Allow inbound TCP 7777 through the host's firewall and make sure nothing
else is bound to that port. Both machines must be on the same subnet.

**What are these `.part` files?**
Leftovers from interrupted transfers. They're never renamed into place, so
they're always safe to delete.

**Where did my files go?**
Into `--save-dir`, which defaults to the directory the receiver was started
in. Folders appear under their own name.

## Development

```bash
go build ./...        # compile
go vet ./...          # static analysis
gofmt -l .            # formatting check
go test ./...         # unit + integration tests
go test -race ./...   # concurrency checks
golangci-lint run     # lint
```

The integration suite starts a real host and receiver over loopback TCP and
covers byte-exact file delivery, rejects, three receivers pulling the same
offer at once, folder transfers with duplicate-name renames, zero-byte
files, the shared feed, wrong-key rejection, and a fake host that tries
reflected and forged auth tags (both must fail).

## Roadmap

- [ ] v0.1.0 tag, served by the Go module proxy
- [ ] Prebuilt binaries and a Homebrew tap, if anyone actually wants them
- [ ] Transfer resume
- [ ] Receiver-to-host acks so host-side bars show confirmed delivery
- [ ] Optional room capacity limit

## License

MIT. See [LICENSE](LICENSE) for the full text.