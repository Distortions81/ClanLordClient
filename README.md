# ClanLord Client

This repository contains source for the open source Clan Lord clients.  A minimal
Go program under `go_client/` demonstrates how to connect to the game servers.

## Demo characters

The game offers a set of free demo characters accessible using the special
account name `demo` and password `demo`.  You can list the available demo
characters with the Go client:

```bash
go run ./go_client -list-demo
```

To log in as one of the demo characters just provide its name along with the
password `demo`:

```bash
go run ./go_client -name "Agratis One" -pass demo
```

The default server address is `server.deltatao.com:5010` and can be overridden
with the `-host` flag.
