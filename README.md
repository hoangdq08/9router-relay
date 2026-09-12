# 9router-relay

A small TCP relay for routing local OpenAI-compatible clients to a remote 9router endpoint.

The relay copies bytes in both directions. It does not parse HTTP, add authentication, terminate TLS, retry requests, or modify payloads. API keys stay in the calling client's configuration, not in this project.

## Requirements

- Go 1.22 or newer
- TCP access to the upstream endpoint

## Build and test

```sh
go test ./...
go build -o bin/9router-relay ./cmd/9router-relay
```

## Run

```sh
./bin/9router-relay \
  -listen 127.0.0.1:20129 \
  -upstream 161.248.239.201:20128
```

Defaults match the existing local Python relay. Do not run both implementations on port `20129` at the same time. For a non-disruptive test, use another local port:

```sh
./bin/9router-relay -listen 127.0.0.1:20131
curl -I http://127.0.0.1:20131/
```

Flags take precedence over environment variables:

| Setting | Flag | Environment variable | Default |
| --- | --- | --- | --- |
| Listen address | `-listen` | `ROUTER_RELAY_LISTEN_ADDR` | `127.0.0.1:20129` |
| Upstream address | `-upstream` | `ROUTER_RELAY_UPSTREAM_ADDR` | `161.248.239.201:20128` |
| Dial timeout | `-dial-timeout` | none | `10s` |
| Debug logging | `-v` | none | disabled |

`-version` prints the build version. Set it during a release build with:

```sh
go build -ldflags "-X main.version=v0.1.0" -o bin/9router-relay ./cmd/9router-relay
```

SIGINT and SIGTERM close the listener and active connections, then wait for handlers to exit.

## macOS LaunchAgent

`deploy/com.jcode.9router-relay.plist.template` uses the same LaunchAgent label as the existing Python relay. Replace `BIN_PATH` and `HOME_DIR` with absolute paths. `launchd` does not expand `~` or `$HOME` inside a plist.

Before switching:

1. Build and test the Go binary on an alternate port.
2. Back up the existing Python source and installed plist outside this repository.
3. Stop the existing LaunchAgent.
4. Install the rendered plist pointing to the Go binary, then load it.
5. Verify the listener and an actual client request through `http://127.0.0.1:20129/v1`.
6. Keep the previous source/plist available for rollback.

This project does not install, stop, or replace the existing service automatically.

## Security

- The default listener is loopback-only. Do not bind to `0.0.0.0` unless network exposure is intentional and protected externally.
- The default upstream uses plaintext TCP/HTTP. This relay does not add encryption. Use a trusted network or an encrypted tunnel for sensitive traffic.
- Do not commit API keys, client auth files, `.env` files, logs, or transcripts.
- Logs contain connection errors and endpoint metadata, not payloads or request headers.

## Scope

This is a transport relay, not an HTTP proxy or a 9router server. It deliberately does not cache, inspect, or retry application requests.
