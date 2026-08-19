# RESTForge (Go/Charm client) - AGENTS.md

A generic Siren hypermedia browser for the terminal, in Go — the third
RESTForge implementation, alongside `../pebble` and `../flutter`.

- What all three apps are and how they relate: [../README.md](../README.md)
- The rules all three are held to: [../AGENTS.md](../AGENTS.md)
- The design and its reasoning, written down once for all three:
  [../docs/DESIGN.md](../docs/DESIGN.md)

This is scaffolding only: `go.mod`, the `Justfile` and an empty
`internal/` package skeleton (`config`, `siren`, `http`, `url`, `render`,
`nav`, `action`, `live`, `tui`), plus a placeholder `main.go` that prints a
message and exits. No command tree, no HTTP, no rendering yet.

- `just run`: run the placeholder from source
- `just test`, `just check`, `just check-code`
- `just build`, `just install`, `just clean`

Status: scaffolding only. This file is a stub — a later task fills it out
with the real workflow, layout and conventions, mirroring
[`../flutter/AGENTS.md`](../flutter/AGENTS.md)'s depth once there is
something to document.

Last updated: August 19, 2026
Maintained for: RESTForge agents
