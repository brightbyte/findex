# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Module

The Go module is named `findex` (see `go.mod`). The main binary lives at `cmd/findex/`.

## Build & Run

```sh
go build ./...
go run ./cmd/findex/ <directory>
```

## Tests

```sh
go test ./...                  # all tests
go test ./pkg/scanner/         # single package
```

## Lint

```sh
go vet ./...
```

## Architecture

The tool walks a directory tree and records each file it finds. Two packages:

- **`pkg/scanner`** — wraps `filepath.WalkDir` to recurse a directory; delegates each file entry (path + size) to a `Recorder`.
- **`pkg/recorder`** — receives file records from the scanner and outputs them (currently prints path and byte size to stdout).

`cmd/findex/main.go` wires them together: constructs a `Recorder`, passes it to a `Scanner`, and calls `Scan` on the target directory.
