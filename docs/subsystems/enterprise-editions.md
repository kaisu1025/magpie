# Enterprise editions

The server holds company API credentials, model routes and employee gateway
keys. The desktop uses a Remote Magpie provider and the employee's gateway
key to connect agents through its local model relay. On Windows the relay
also serves WSL agents using the WSL network and its dedicated key.

The [edition selector](../../internal/edition/edition.go) accepts
`enterprise-server` and `enterprise-desktop`. The Docker image and a
plain build default to the server. `make enterprise-desktop` selects the
desktop edition at build time.

The server retains the gateway, routing, groups, budget, rotation, usage
and request masking modules. The desktop retains the gateway for its
local model relay and Usage for this computer's requests. The plugin
marketplace, browser administration connection, standalone session
management, Library pages, and cloud setup
sync are outside the enterprise builds. **Usage → Sessions** lists and
summarizes sessions seen by the server's gateway. Request archives are
stored locally.

The server's Settings page covers display preferences, usage display,
gateway networking, model routing and request masking. The desktop's
Settings page covers display preferences, local startup and relay
controls, the relay port and proxies. Both show version, gateway address
and diagnostics under **About**. The server continues to issue employee
keys from **Gateway**, and the desktop configures its enterprise
connection under **Providers → Remote magpie**.

## Server

Build and run the server with Docker. This example publishes the admin
page on the Docker host's loopback and the model API on the network.
Set `MAGPIE_WEB_KEY` in the shell to a separate admin secret before
starting the container.

```sh
docker build --target enterprise-server -t magpie-enterprise-server .
docker run -d --name magpie-enterprise \
  -p 3425:3425 -p 127.0.0.1:3430:3430 \
  -e MAGPIE_WEB_KEY -v magpie-enterprise:/config \
  magpie-enterprise-server
```

When Docker Hub is unreachable, pass `--build-arg GO_IMAGE=...` and
`--build-arg RUNTIME_IMAGE=...` to build from your registry mirror.

Open the admin page on port 3430 with the link printed by the server.
In **Providers**, add company API credentials and models. In **Routing
groups**, set the primary and backup models under a stable name such as
`group/coding-default`. In **Gateway → Gateway keys**, issue a key for
each employee and allow the intended models. Give employees the network
address on port 3425 and their own gateway key. Use the existing TLS flags
or your HTTPS ingress when publishing the API to employees.

The [gateway's listener](../../internal/gateway/lan.go) checks model API
keys even on loopback in the server edition. This covers requests delivered
by a local TCP forward. A removed key is refused on the next request.
Gateway health checks still use the listener's root. The existing usage
ledger and admin page attribute calls to the employee key.

## Desktop

Run `make enterprise-desktop` on the target OS. It creates
`dist/magpie-enterprise-desktop` (`.exe` on Windows).
To pilot alongside a personal Magpie install, use a separate
`XDG_CONFIG_HOME` and choose a free port with `MAGPIE_ADDR`.
The desktop uses the specified port on `127.0.0.1`.

The desktop's **Providers** page offers **Remote magpie**. Enter the
enterprise gateway URL and employee key, then verify the connection and
expose the models. The server manages upstream API keys and model routes.
**Agents** uses the [same agent detection and connection handlers](agent-wiring.md)
as Magpie, including the built-in agents, WSL agents, and installed agent
modules. Connecting writes the agent's local gateway configuration. The
Disconnect action restores its previous active routing settings. Codex
keeps the provider entry so threads created with it can still open.
The management page connection and browser sharing are omitted in this edition.

The tray panel offers **Agents** and **Usage**. Usage summarizes this
computer's gateway and agent session logs. **Open Usage** opens the main window's **Usage** page
with the selected period, provider and day. The main page shows the local
request ledger, including its charts and CSV export.

## Verify a pilot

1. Use Claude Code, Codex, and another installed agent with a permitted model.
2. Force the primary API to return a retryable failure and verify that
   the call succeeds through the backup.
3. Check the employee key's usage in the admin page, then disable or
   remove the key and verify that a new request is rejected.
4. Disconnect both clients and verify their active provider and model
   settings return to the pre-connection values.

The [loopback regression](../../internal/gateway/enterprise_test.go)
exercises the listener, upstream credential separation, usage attribution,
and key revocation. The existing agent disconnect and gateway fallback
tests cover the reused paths.
