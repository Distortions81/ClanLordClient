# AGENT INSTRUCTIONS

This repository contains two client implementations for the game **Clan Lord**:

- `go_client/`: a minimal Go client using Ebiten.
- `mac_client/`: the full macOS client built with Xcode.

Below are build and test instructions for future tasks.

## Building the Go client

1. Install system dependencies (Ubuntu/Debian):
   ```bash
   sudo apt-get update
   sudo apt-get install -y libgl1-mesa-dev libxrandr-dev libxinerama-dev libxcursor-dev libxxf86vm-dev libxi-dev libxfixes-dev
   ```
2. Build the project:
   ```bash
   cd go_client
   go build ./...
   ```
   This compiles all Go packages. It may download modules on the first run.

## Running Go tests

The Go code currently has no test files, but the following command can be used to confirm:
```bash
cd go_client
go test ./...
```
It should report `[no test files]`.

## Building the macOS client

The macOS client resides in `mac_client/client` and is built using Xcode.
On macOS with Xcode installed run:
```bash
cd mac_client/client
./makeit.sh
```
This invokes `xcodebuild` to produce debug and release builds.

## Notes

- There are no automated tests for the macOS client.
- Scripts under `scripts/` are utilities and not part of the build.
