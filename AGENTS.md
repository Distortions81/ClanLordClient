# AGENT INSTRUCTIONS

This repository contains two client implementations for the game **Clan Lord**:

- `go_client/`: a minimal Go client using Ebiten.
- `mac_client/`: the full macOS client built with Xcode, used as reference for building a new alterate client in golang.

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

## Notes

- Scripts under `scripts/` are utilities and not part of the build.
