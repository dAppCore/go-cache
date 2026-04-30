# go-cache

`dappco.re/go/cache` provides a storage-agnostic cache backed by
`dappco.re/go/io` media. It stores JSON entries, binary sidecars, scoped caches,
and request/response metadata for HTTP cache workflows.

## Development

The module lives in `go/`.

```sh
cd go
GOWORK=off GOPROXY=direct GOSUMDB=off go test -count=1 -short ./...
```

The repository is v0.9.0 audit-driven; the release contract is:

```sh
bash /Users/snider/Code/core/go/tests/cli/v090-upgrade/audit.sh .
```
