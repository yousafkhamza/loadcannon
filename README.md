# loadcannon

Load-test internal and public HTTP APIs from one scenario format. Wraps [k6](https://k6.io) for the actual traffic generation; loadcannon handles config, target resolution (LB / direct IP / hostname), and secure secret injection. Single static binary, zero third-party Go dependencies.

## Quickstart

```bash
curl -fsSL https://yousafkhamza.github.io/loadcannon/install.sh | bash

# the binary alone has no scenario files on disk — pull the bundled examples out first
loadcannon examples --write scenarios

cp scenarios/example-public-https-domain.json my-api.json
# edit my-api.json: target.url, auth, and the endpoint paths for your API

loadcannon validate --scenario my-api.json   # one request, sanity check before spending VU-minutes
loadcannon run      --scenario my-api.json   # the real load test
open loadcannon-out/report.html              # results
```

`loadcannon --help` prints this same sequence any time you need a reminder.

## Install

```bash
curl -fsSL https://yousafkhamza.github.io/loadcannon/install.sh | bash
```

Or download a release binary directly from [Releases](https://github.com/yousafkhamza/loadcannon/releases).

You also need **k6** on the machine that actually runs the load (not needed just for `validate` or `gen-k6`): https://k6.io/docs/get-started/installation/

## The three ways you'll point it at something

| Scenario | `target.url` | `target.host_override` | `target.insecure_skip_verify` |
|---|---|---|---|
| Public API by hostname | `https://api.example.com` | unset | `false` |
| Internal API via internal LB/DNS | `https://internal-lb.company.internal` | unset | usually `false` (internal CA) |
| Direct IP, bypassing DNS/LB round-robin, testing one node | `https://10.20.4.17` | the real hostname, e.g. `internal-lb.company.internal` | usually `true` (cert CN won't match the IP) |

`host_override` sets both the HTTP `Host` header and the TLS SNI/ServerName, so hitting a bare IP still routes correctly on name-based virtual hosting / ALB listener rules and still presents the right cert for the check k6/your Go baseline request expects.

## Which example file to copy

`scenarios/` ships six ready-to-copy files. Five of the six point at real, live, public test APIs — you can `loadcannon run` them right now, no account or real secrets needed — covering every target/auth combination you'll actually hit. If you installed just the binary (not the repo), get them with `loadcannon examples --write scenarios` — they're embedded in the binary itself:

| File | Target | Auth |
|---|---|---|
| `example-public-https-domain.json` | `jsonplaceholder.typicode.com` — public HTTPS, real live test API | **none needed** |
| `example-public-http-domain.json` | `httpbin.org` — public, plain HTTP (no TLS) | **none needed** |
| `example-public-direct-ip.json` | Cloudflare `1.1.1.1` hit directly by IP, `host_override` + `insecure_skip_verify` demonstrated | **none needed** |
| `example-internal-lb-domain.json` | `httpbin.org/bearer` as a public stand-in for an internal LB domain | **token required** — any non-empty value works (`token_source: env`, `DEMO_TOKEN`) |
| `example-internal-direct-ip.json` | Google `8.8.8.8` as a public stand-in, direct IP + `host_override`, no LB | **none needed** |
| `example-internal-template.json` | **Blank placeholder template** — copy this per real internal service you onboard | `bearer` via `ssm`, all fields are `<PLACEHOLDER>` |

The two marked "internal-style" use public IPs/domains as honest stand-ins so you can see the whole tool work end-to-end immediately — swap the URL for your real internal target once you're ready. Only `example-internal-template.json` is a non-runnable placeholder.

`example-internal-template.json` intentionally has an extra `_comment` field, which loadcannon rejects with a clear parse error until you delete it — so the template can never accidentally run as-is. Copy it, replace every `<PLACEHOLDER>`, delete `_comment`, then `loadcannon validate` it.

```bash
cp scenarios/example-internal-template.json scenarios/checkout-svc.json
# edit checkout-svc.json: fill in the LB/IP, token_ref, endpoint paths, delete "_comment"
loadcannon validate --scenario scenarios/checkout-svc.json
```

`host_override` only needs to be set when `target.url` is a bare IP; leave it empty for a normal hostname target.

**Reaching an internal endpoint without a VPN:** if the target sits in a private subnet and you don't have VPN access from where you're running loadcannon, use `scripts/tunnel-ssm.sh` to open an IAM-authenticated SSM port-forward through an instance that already has network reachability (a bastion, or the app instance itself) — no inbound security group changes needed:

```bash
./scripts/tunnel-ssm.sh -i i-0abcd1234ef567890 -r me-central-1 \
  -h internal-lb.company.internal -p 443 -l 8443
```

Then point `target.url` at `https://localhost:8443` with `host_override` set to the real internal name and `insecure_skip_verify: true`.

## Finding the endpoints to test

loadcannon doesn't discover endpoints for you — feed it what you already know is in scope:

- **From your API Gateway / ALB config** — list routes/target groups (`aws apigateway get-resources`, or your ALB listener rules) and turn each into a `scenarios[]` entry.
- **From an OpenAPI/Swagger spec**, if the service publishes one — each path/method pair becomes a scenario.
- **From your own discovery tooling** — if you've already run something like your ffuf/Burp-style active-discovery pass, its output of live, non-catch-all paths is exactly the scenario list here.

Keep the traffic mix realistic with `weight` per scenario rather than hammering every endpoint equally.

## Auth — how secrets stay out of the repo and off the wire in plaintext logs

Scenario files hold **references**, never plaintext secrets:

```json
"auth": {
  "mode": "bearer",
  "token_source": "ssm",
  "token_ref": "/company/loadtest/checkout-svc-token"
}
```

`token_source` / `username_source` / `password_source` can be:

- **`env`** — read from an environment variable by name. Good for CI where the secret is injected as a masked pipeline variable.
- **`file`** — read from a file path (e.g. a secret mounted read-only by your CI runner, `chmod 600`).
- **`ssm`** — resolved via `aws ssm get-parameter --with-decryption`, reusing whatever AWS role/credentials are already on the host. Matches the SecureString pattern already used for `manage-users.sh`.
- **`prompt`** — interactive, terminal-echo disabled, for ad-hoc runs where you don't want the token sitting in an env var at all.

At run time the resolved value is:

- injected into the generated k6 script **only** via `__ENV` (never string-substituted into the `.js` file itself)
- passed to the `k6` subprocess **only** through its environment block — never as a CLI argument, so it never shows up in `ps aux` or shell history
- never written to `loadcannon-out/` or any log file

Supported `auth.mode` values: `none`, `bearer`, `basic`, `apikey`.

## Usage

```bash
# 1. sanity check before spending VU-minutes on a typo
loadcannon validate --scenario scenarios/example-internal-lb-domain.json

# 2. run it
loadcannon run --scenario scenarios/example-internal-lb-domain.json
#   -> loadcannon-out/script.js, summary.json, report.html

# override load shape for a quick smoke run
loadcannon run --scenario scenarios/example-public-https-domain.json --vus 5 --duration 15s

# just want the k6 script, e.g. to hand to a CI job that runs k6 itself
loadcannon gen-k6 --scenario scenarios/example-public-https-domain.json -o script.js

# re-render a report from an existing k6 summary
loadcannon report --summary loadcannon-out/summary.json -o report.html
```

Pass extra raw flags straight through to k6 with `--k6-arg`, e.g. `--k6-arg --out --k6-arg influxdb=http://localhost:8086`.

## Scenario file schema

See `scenarios/example-internal-lb-domain.json`, `scenarios/example-internal-direct-ip.json`, `scenarios/example-public-https-domain.json`. Shape:

```json
{
  "name": "string",
  "target": { "type": "internal|public", "url": "https://...", "host_override": "", "insecure_skip_verify": false },
  "auth": { "mode": "none|bearer|basic|apikey", "header": "Authorization", "prefix": "Bearer ",
            "token_source": "env|file|ssm|prompt", "token_ref": "..." },
  "scenarios": [
    { "name": "string", "method": "GET", "path": "/v1/x", "weight": 1, "expect_status": 200, "body": "{...}" }
  ],
  "load": {
    "vus": 10, "duration": "30s",
    "stages": [{ "duration": "30s", "target": 20 }]
  },
  "thresholds": { "http_req_duration": "p(95)<500", "http_req_failed": "rate<0.01" }
}
```

`load.stages` (ramp profile) takes precedence over flat `vus`/`duration` when both are present.

JSON rather than YAML is intentional — it keeps the binary at zero third-party Go dependencies (no supply-chain surface from a YAML parser), which also means `go build` needs no network access, useful on locked-down CI runners.

## Release process

Tag-driven, via [goreleaser](https://goreleaser.com):

```bash
make tag VERSION=v1.1.0
```

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which cross-builds linux/darwin/windows × amd64/arm64 binaries, checksums them, and publishes a GitHub Release. `scripts/install.sh` fetches whatever `latest` (or a pinned tag) resolves to.

## CI/CD

- `.github/workflows/ci.yml` — gofmt/vet/build on every push and PR, plus a sanity check that the generated k6 script is valid JS
- `.github/workflows/release.yml` — goreleaser cross-platform release on `v*` tags
- `.github/workflows/pages.yml` — deploys `docs/` (landing page + `install.sh`) to GitHub Pages on changes to `docs/`

## Known limitations

- `body_file` in a scenario isn't inlined by `gen-k6` yet — use an inline `"body"` JSON string for now.
- Masked `prompt` auth relies on `stty`; on a shell without a real tty (some CI runners) it falls back to visible input with a warning — use `env` or `file` there instead.
- No distributed/multi-runner load generation built in; for that scale, feed the generated script (`gen-k6`) into k6's own cloud or Kubernetes operator.

## License

MIT
