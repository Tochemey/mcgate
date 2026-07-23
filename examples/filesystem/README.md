# Filesystem Example

This example shows a minimal end-to-end workflow with **portcullis**: configure a gateway with one MCP tool (the filesystem server), start the gateway, list registered tools, and invoke the `list_directory` tool to list the contents of a directory.

## What It Demonstrates

- **Gateway setup** — Building a `mcp.Config` with a single stdio-based MCP tool.
- **Gateway lifecycle** — Creating the gateway with `portcullis.New`, starting it with `Start`, and stopping it with `Stop`.
- **Tool discovery** — Calling `ListTools` to see what tools the gateway has registered after bootstrap.
- **Tool invocation** — Calling `Invoke` with an `mcp.Invocation` that specifies the tool, the MCP method, parameters (tool name + arguments), and correlation metadata (tenant, client, request ID).
- **Result handling** — Reading the `ExecutionResult` (status, duration, optional error, and output map) and printing the directory listing.

The MCP tool used here is the official **Model Context Protocol filesystem server** (`@modelcontextprotocol/server-filesystem`). It runs as a child process and is communicated with over stdin/stdout (stdio transport). The gateway supervises this process, manages sessions per tenant+client, and routes invocations to it.

## Prerequisites

- **Go** 1.21+ (or the version required by the portcullis module).
- **Node.js** and **npx** — the example launches the filesystem MCP server via `npx -y @modelcontextprotocol/server-filesystem <root>`.

## How to Run

From the **repository root**:

```bash
make -C examples/filesystem run
# or
go run ./examples/filesystem
```

From **anywhere** (module path):

```bash
go run github.com/tochemey/portcullis/examples/filesystem
```

Build a binary:

```bash
make -C examples/filesystem build
./examples/filesystem/bin/filesystem-example
```

## Environment Variables

| Variable      | Description                                                                                                                                                                                                             |
|---------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `MCP_FS_ROOT` | Directory root exposed to the filesystem MCP server. Defaults to the **current working directory** when unset. On macOS, use `MCP_FS_ROOT=/private/tmp` to list `/tmp` (because `/tmp` is a symlink to `/private/tmp`). |

## Makefile Targets

Run `make help` in this directory (or `make -C examples/filesystem help` from the repo root):

- **run** — Run the example (default root: current directory).
- **build** — Build the binary into `bin/filesystem-example`.
- **run-macos** — Run with `MCP_FS_ROOT=/private/tmp` (useful to list `/tmp` on macOS).
- **clean** — Remove the `bin/` directory.

## What You’ll See

1. **Log output** from the portcullis gateway (actor system, registrar, router, tool supervisor, etc.).
2. A header: `=== portcullis filesystem example ===` and the chosen filesystem root.
3. **Registered tools** — A count and list of tool IDs and transports (e.g. `filesystem (stdio)`).
4. **Invocation result** — Status (`success` or `failure`), duration, and optionally an error message.
5. **Output** — JSON containing the MCP tool response; for `list_directory` this includes a `content` array with text listing directory entries (e.g. `[FILE] ...`, `[DIR] ...`).

## Code Flow (High Level)

1. **Config** — Set the filesystem root (env or `.`), then build `mcp.Config` with one tool: ID `"filesystem"`, transport `stdio`, and stdio config that runs `npx -y @modelcontextprotocol/server-filesystem <root>`.
2. **Gateway** — `portcullis.New(cfg)` creates the gateway; `gw.Start(ctx)` starts the actor system and bootstraps tools (spawns the filesystem child process and registers the tool).
3. **Warm-up** — A short sleep allows foundational actors (registrar, router, tool supervisor) to finish spawning before we call `ListTools` and `Invoke`.
4. **ListTools** — `gw.ListTools(ctx)` returns all registered tools (here, just the filesystem tool).
5. **Invoke** — `gw.Invoke(ctx, inv)` sends an invocation for tool `"filesystem"`, method `"tools/call"`, with params `name: "list_directory"` and `arguments: { path: root }`, plus correlation (tenant, client, request ID). The gateway routes the request to the appropriate session, which talks to the MCP server over stdio.
6. **Output** — Print the result status, duration, any error, and the JSON output (directory listing).
7. **Shutdown** — `defer gw.Stop(ctx)` stops the gateway and the child process when the program exits.

## Related

- [portcullis README](../../README.md) — Gateway API, configuration, and options.
- [MCP Filesystem Server](https://github.com/modelcontextprotocol/servers/tree/main/src/filesystem) — The MCP server used by this example.
