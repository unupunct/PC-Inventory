# Security

## Threat model

PC Inventory Manager is designed for a trusted private network: a home, a home lab or a small office LAN. It is **not** hardened for exposing the dashboard directly to the internet.

What the design defends against:

| Threat | Mitigation |
|---|---|
| A rogue computer enrolling itself and injecting fake inventory | Enrollment requires administrator approval before the agent receives a token |
| A stolen or leaked agent token | Per-agent tokens, individually revocable; rotation invalidates the previous token after a grace window |
| Token theft from server storage | Only SHA-256 hashes of tokens are stored; the plain token exists on the agent and briefly server-side until the agent collects it |
| Password guessing against the dashboard | argon2id password hashing, per username+IP lockout after 5 failed attempts for 5 minutes |
| Session hijacking | HTTP-only, SameSite=Lax session cookies; sessions expire (default 7 days) and are stored server-side so they can be invalidated |
| Tampering with audit history | Every administrative action is written to an append-only audit table with actor, action, target, client IP and time |
| Network scanning from outside | The server binds to `127.0.0.1` unless you explicitly set `LISTEN_HOST`; optional `ALLOWED_CIDRS` allow list |

## What is never collected

The agent collects hardware, OS, network and software *inventory*. It never reads:

- passwords, password managers, keychain or credential stores
- browser history, cookies, saved logins or form data
- personal files, documents, photos or email content
- Wi-Fi passwords (the SSID is read on macOS/Linux; the key is not)
- keystrokes, screen contents or clipboard

Installed-application inventory reads registry uninstall entries on Windows and the package database on Linux — the same data the OS lists in "Add or remove programs".

## Credentials

**Agent tokens** are 32-byte random values, base64url-encoded. The server stores `sha256(token)`; the first 8 characters (`tokenPrefix`) are kept in clear for display in the dashboard so an operator can tell which token is which without being able to reuse it.

**Token lifecycle**

1. Enrollment creates the agent record and a token that is held server-side until the agent collects it.
2. Approval (or auto-approve) makes the agent active.
3. `POST /api/agents/{id}/rotate-token` issues a new token; the previous hash stays valid for 10 minutes so an in-flight report is not lost.
4. `POST /api/agents/{id}/revoke` invalidates the current token immediately; the agent must be approved again.

**Admin passwords** are hashed with argon2id (RFC 9106 defaults) and never logged. Change the bootstrap password immediately after first login; delete `data/initial-admin-password.txt`.

## Transport

Plain HTTP is acceptable only on a network you control. On anything else, enable TLS:

```bash
PCIM_TLS_CERT=server.crt PCIM_TLS_KEY=server.key ./pcim-server
```

Agents verify certificates normally. `--insecure` disables verification and should only be used with a self-signed certificate on a trusted LAN; it is recorded in the agent configuration file.

## Remote actions

The server can ask an agent to do exactly three things:

- `inventory_refresh` — re-read hardware and report
- `rotate_token` — adopt a new API token
- configuration update — adopt new intervals/toggles

There is **no** remote shell, script execution, file transfer, reboot or shutdown. This is deliberate: a compromised server should not be able to run commands on every computer in the house.

## Hardening checklist

- [ ] Set a strong admin password and delete the bootstrap file
- [ ] Enable TLS, or keep `LISTEN_HOST=127.0.0.1` and reach the dashboard over a tunnel/VPN
- [ ] Restrict the firewall rule to your private network profile only
- [ ] Set `ALLOWED_CIDRS` to your LAN range
- [ ] Turn off auto-approve once your computers are enrolled
- [ ] Review the Enrollments page occasionally for unfamiliar hosts
- [ ] Keep backups on a separate disk and verify a restore
- [ ] Update both binaries when a new version is released

## Auditing

Dashboard: Settings → Audit log. API: `GET /api/audit`.

Recorded events include `auth.login`, `auth.login.failed`, `agent.enroll.request`, `agent.enroll.complete`, `agent.approve`, `agent.reject`, `agent.revoke`, `agent.token.rotate`, `agent.delete`, settings changes and backup creation.

## Reporting a vulnerability

This is a self-hosted personal project; there is no formal security team. If you find a flaw, open an issue describing the impact and reproduction steps without including any real credentials or inventory data.
