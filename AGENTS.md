# AGENTS

This repo includes a minimal Go client under `go_client/`. To build or run the Go program you need Go version 1.24 or later.

## Installing dependencies

1. Install Go 1.24 or later. On Debian/Ubuntu you can run:
   ```bash
   sudo apt-get update
   sudo apt-get install -y golang-go build-essential libgl1-mesa-dev libglu1-mesa-dev xorg-dev
   ```
   `libgl1-mesa-dev`, `libglu1-mesa-dev`, and `xorg-dev` provide the OpenGL and X11 libraries required by Ebiten.
   On other distributions install the equivalent development packages.
2. Fetch Go module dependencies:
   ```bash
   cd go_client
   go mod download
   ```

## Build steps
1. Navigate to the `go_client` directory if not already there:
   ```bash
   cd go_client
   ```
2. Compile the program:
   ```bash
   go build
   ```
   This produces the executable `go_client` in the current directory.
3. You can also run the program directly with:
   ```bash
   go run .
   ```

The module path is `go_client` and the main package is located in this directory.

The `mac_client` directory contains a reference implementation written in C.
