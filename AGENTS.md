# go-cache Agent Notes

This repository follows the core/go v0.9 layout:

- Go module source lives under `go/`.
- Root `go.work` lists `./go` and local dependency checkouts under `external/`.
- Do not edit `.core/` runtime configuration.
- Do not edit source inside `external/`; only submodule pointers should change.

Use `core.Result` for fallible package APIs and core/go wrappers for filesystem,
JSON, process, string, and environment operations.
