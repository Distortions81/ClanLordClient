# AGENTS

This repo includes a minimal Go client under `go_client/`. To build or run the Go program you need to have Go (version 1.24 or later) installed.

## Build steps
1. Navigate to the `go_client` directory:
   ```bash
   cd go_client
   ```
2. Fetch all dependencies specified in `go.mod`:
   ```bash
   go mod download
   ```
3. Compile the program:
   ```bash
   go build
   ```
   This produces the executable `go_client` in the current directory.
4. You can also run the program directly with:
   ```bash
   go run .
   ```

The module path is `go_client` and the main package is located in this directory.

the mac_client is our reference, written in C.