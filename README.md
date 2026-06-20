# qbt

Drive multiple qBittorrent instances as if they were one.

## Install

```sh
go install github.com/aca/qbittorrent-cli/qbt@latest
```

## Configure

`~/.config/qbt/config.json`:

```json
{
  "instances": [
    { "name": "box1", "host": "http://box1:8080", "username": "admin", "password": "pw", "tls_skip_verify": true  },
    { "name": "box2", "host": "http://box2:8080", "username": "admin", "password": "pw", "tls_skip_verify": true }
  ]
}
```

## Usage

```sh
qbt                       # interactive TUI (default)
qbt list                  # merged torrent list from all instances
qbt add <magnet|url|file> # add to the instance with the most free space
qbt pause  <hash>...
qbt resume <hash>...
qbt delete <hash>... [--delete-files]
qbt dedup                 # find cross-instance duplicates, delete lower-progress copies (--yes to apply)
```

Common flags: `--instance <name>` to scope to one instance, `--config <path>`, `-v` for verbose logs.

### TUI keys

`↑/↓` move · `/` search · `p` start · `s` stop · `d` delete · `D` delete+files · `r` refresh · `q` quit
