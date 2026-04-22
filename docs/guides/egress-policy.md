# Egress policy

By default, guests can reach anything the host can. Set
`ORCHESTRATOR_EGRESS_ALLOWLIST` to restrict that to a specific set of hosts.

## How it works

When the allowlist is non-empty, orchestrator installs **per-TAP FORWARD
rules** that:

1. Permit DNS (UDP + TCP port 53) to anywhere.
2. Permit `api.anthropic.com` (resolved to IPs at rule-install time).
3. Permit every entry in your allowlist (IPs, CIDRs, hostnames — hostnames
   resolved at install).
4. Default-DROP everything else originating from that TAP.

Each rule is tagged with a comment `orch-egress-<tapdev>` so teardown can
find and remove them.

## Syntax

Comma-separated values. Any of:

- IP address: `52.89.21.44`
- CIDR: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.1.0/24`
- Hostname: `github.com`, `api.openai.com`

```bash
ORCHESTRATOR_EGRESS_ALLOWLIST="github.com,api.openai.com,10.0.0.0/8"
```

## Common recipes

### Only Anthropic + GitHub

```bash
ORCHESTRATOR_EGRESS_ALLOWLIST=github.com,api.github.com,codeload.github.com
```

(`api.anthropic.com` is always allowed when the allowlist is non-empty.)

### Only Anthropic — no network otherwise

Empty allowlist disables the feature entirely (default). To allow only
Anthropic, set a placeholder IP that'll never resolve:

```bash
ORCHESTRATOR_EGRESS_ALLOWLIST=0.0.0.0/32
```

Better: set it to `api.anthropic.com` explicitly (harmless duplicate):

```bash
ORCHESTRATOR_EGRESS_ALLOWLIST=api.anthropic.com
```

### Corporate allowlist

```bash
ORCHESTRATOR_EGRESS_ALLOWLIST="\
internal.corp.net,\
packages.corp.net,\
registry.npmjs.org,\
github.com,\
api.github.com,\
raw.githubusercontent.com,\
codeload.github.com,\
objects.githubusercontent.com,\
files.pythonhosted.org,\
pypi.org"
```

(Commas inside the env var value work fine — the example is line-broken
for readability with trailing backslashes.)

## Gotchas

### DNS resolution is one-shot

Hostnames are resolved **at rule insertion**, not at packet time. If
`github.com` resolves to a different IP tomorrow, your rule stops matching.
Workarounds:

- Prefer CIDR entries for hostnames with stable address space (e.g.,
  GitHub's `140.82.112.0/20`).
- Rotate VMs on a schedule so the rules are refreshed.
- Accept that rare IP changes will cause one-off failures.

### IPv6

The iptables bindings are v4-only. If a hostname resolves to an IPv6
address, we emit a `/128` rule into the v4 chain and it does nothing. The
guest falls back to v4 via NAT so this is usually invisible. If your
allowlist target is v6-only, the guest can't reach it.

### UFW

UFW's FORWARD default is DROP. Orchestrator inserts its rules at position 1.
The default-DROP at the end of orchestrator's egress rules is
`-A FORWARD`, so it lands after any UFW rules already present — which is
fine, because UFW rules are bounded to specific interfaces that aren't our
TAP.

### Auditing

```bash
sudo iptables -L FORWARD -n -v --line-numbers | grep orch-egress
```

Shows every rule the orchestrator added, with byte + packet counters so you
can see what's actually being hit.

### When a guest can't reach a host

1. `sudo iptables -L FORWARD -n -v | grep orch-egress` — rules there?
2. From the guest (`vm exec --name … --command "getent hosts <hostname>"`):
   does DNS resolve?
3. From the guest: `curl -v <url>` — the error message tells you.
4. `sudo iptables -L FORWARD -n -v | grep DROP` — is the default-DROP
   catching your packet? If so, `<hostname>` wasn't resolved to the current
   IP at rule-install time.

## Performance

Each additional allowlist entry is one iptables rule. Even hundreds of
entries don't meaningfully affect throughput on a 1 Gb link, but they do
add rule-insertion time at VM create (a few ms per rule). Keep the list
short unless you have a reason.
