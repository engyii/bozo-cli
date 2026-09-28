# bozo

Unofficial command-line client for Zoho apps. Not affiliated with Zoho.

**Alpha:** commands, flags and `--json` output can change between releases.
Issues and pull requests are closed for now.

It currently does one thing: list the Cliq channels you joined that have
unread messages, and show those messages. It never marks anything as read.

## Install

Download an archive from
[Releases](https://github.com/engyii/bozo-cli/releases) and put `bozo` on your
PATH. Only Linux amd64 has been tested.

A build from source (`go install github.com/engyii/bozo-cli/cmd/bozo@latest`)
has no built-in OAuth client and needs `bozo auth login --client-id`.

## Use

```
bozo auth login      # once, approve in the browser
bozo cliq unread
bozo cliq unread --count-only --json | jq .total_unread
```

`bozo <command> --help` lists flags and environment variables.

## Develop

```
go test -race ./...
git config core.hooksPath .githooks   # reinstall bozo after each commit and pull
```

A `v*` tag builds a release in GitHub Actions. It needs the repository
secrets `BOZO_CLIENT_ID` and `BOZO_CLIENT_SECRET` and the variable
`BOZO_CLIENT_DC`: the Zoho OAuth client compiled into the binaries.
