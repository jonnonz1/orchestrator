# Networking & vsock

Two channels, doing different jobs:

1. **vsock** connects the orchestrator process on the host to the guest
   agent. Zero packets go on the wire.
2. **A TAP device + iptables NAT** gives the VM internet access so the agent
   inside can `curl`, `apt-get`, `git clone`, etc.

## Why vsock (not SSH)

The agent needs to be reachable before the guest has finished booting.
Over SSH that means:

- SSH daemon has started
- Host keys generated
- Firewall allows inbound
- Network config applied

…which happens in the last 500 ms of a ~4-second boot. Over vsock:

- Kernel loaded vsock module in the first ~50 ms of boot
- Agent binary started by systemd as soon as its after-dependency is met
- Host orchestrator can `vsock_connect(CID, 9001)` and get a response
  within **<100 ms of the kernel boot**

No keys, no handshake, no network attack surface from the orchestrator's
side. vsock is purely kernel-to-kernel.

The vsock CID is a per-VM unsigned 32-bit integer. Orchestrator derives it
deterministically from the VM name (FNV-32a hash, offset into a safe range)
so the CID is stable across restarts.

## Network topology for one VM

```
  HOST                                           GUEST
┌───────────────────┐   ┌──────────────────┐   ┌──────────────────┐
│ eth0 (LAN)        │   │ fc-<name> (TAP)  │   │ eth0 (inside VM) │
│ 192.168.1.10      │   │ 172.16.<slot>.1  │◄──┤ 172.16.<slot>.2  │
│                   │   │ /24              │   │ /24              │
└─────────┬─────────┘   └────────┬─────────┘   └────────┬─────────┘
          │                      │                      │
          │         iptables NAT (host)                 │
          │                      │                      │
          └──────────┬───────────┘                      │
          ┌──────────▼─────────────────────┐            │
          │ nat:POSTROUTING               │             │
          │   -s 172.16.<slot>.0/24       │             │
          │   -o eth0 -j MASQUERADE       │             │
          └───────────────────────────────┘             │
          ┌────────────────────────────────┐            │
          │ filter:FORWARD (pos 1)        │             │
          │   -i fc-<name> -o eth0 ACCEPT │             │
          │   -i eth0 -o fc-<name>        │             │
          │     RELATED,ESTABLISHED ACCEPT│             │
          └────────────────────────────────┘            │
                                                        │
          ┌─── vsock (no network stack) ────────────────┤
          │    CID=<per-vm>, port=9001                  │
          └─────────────────────────────────────────────┘
```

## Per-VM addressing

- **Subnet:** `172.16.<slot>.0/24`
- **TAP IP (host side):** `172.16.<slot>.1`
- **Guest IP:** `172.16.<slot>.2`
- **TAP device name:** `fc-<vm-name>` (truncated to 15 chars — Linux `IFNAMSIZ`)
- **vsock CID:** FNV-32a hash of the VM name, modded into the legal range.

The `<slot>` is derived from the VM name the same way: FNV-32a hash → mod
255 → that's your second octet. In practice orchestrator manages small
numbers of VMs so collisions are rare, but the bug-tolerant move would be
to plumb a slot registry and retry on collision.

## iptables rule hygiene

The FORWARD rules are **inserted at position 1** for UFW compatibility: if
the host's FORWARD chain defaults to DROP (UFW's default), appended rules
never match because DROP matches first.

Rules are idempotent: `SetupNAT` calls `ipt.Exists` before `ipt.Insert`, so a
retry (e.g., recovery after a crash) does not duplicate rules.

On `Destroy`, rules are removed by matching on the exact tuple. The egress
allowlist uses a **comment tag** (`orch-egress-<tapdev>`) so teardown can
find them even if the exact rule text drifted.

## The egress allowlist

See [Guides → Egress policy](../guides/egress-policy.md) for recipes. The
short version:

- Unset `ORCHESTRATOR_EGRESS_ALLOWLIST` → full outbound (default).
- Set to `api.github.com,10.0.0.0/8` → only those hosts, DNS on UDP/TCP 53,
  plus `api.anthropic.com` are reachable.
- Hostname entries are resolved **once at rule insertion**. If the host's IP
  changes, you need to destroy + recreate the VM (or rotate it on a
  schedule).
- Rules are tagged with `orch-egress-<tapdev>` so they're easy to audit:
  `iptables -L FORWARD -n -v | grep orch-egress`.

## Running behind UFW

UFW's default forward policy is DROP. That's fine — orchestrator inserts
per-VM FORWARD rules at position 1. You need to open the API and MCP ports
explicitly:

```bash
sudo ufw allow 8080/tcp comment 'orchestrator api'
sudo ufw allow 8081/tcp comment 'orchestrator mcp'
```

If you want the dashboard accessible only from your LAN:

```bash
sudo ufw allow from 192.168.0.0/16 to any port 8080 comment 'orchestrator api lan'
sudo ufw deny  8080/tcp comment 'orchestrator api deny'
```

Order matters — the `allow from` must come before the catch-all `deny`.
