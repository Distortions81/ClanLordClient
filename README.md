# ClanLord Client

This repository contains source for the open source Clan Lord clients.  A minimal
Go program under `go_client/` demonstrates how to connect to the game servers.

## Demo characters

The game offers a set of free demo characters accessible using the special
account name `demo` and password `demo`. When these credentials are supplied,
the Go client will select a random demo character to log in as:

```bash
go run ./go_client -name demo -pass demo
```

The default server address is `server.deltatao.com:5010` and can be overridden
with the `-host` flag.

Debug logs are written to `debug-<date>-<time>.log` by default. Use `-debug=false` to disable logging. When the server
responds with `-30972` or `-30973`, the Go client will now fetch updated data
files from the provided URL and reconnect automatically.

If you are missing the `CL_Images` or `CL_Sounds` data files you can force
the server to provide them by specifying an older client version with the
`-client-version` flag.  For example:

```bash
go run ./go_client -client-version 1353
```

The value passed should be the desired `kVersionNumber`.  Older versions
cause the server to send the associated images and sound archives before
reconnecting.
The Go client automatically falls back to this baseline version when it detects missing image or sound data.
