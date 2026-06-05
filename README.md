# envault

Save and restore `.env` files with a single command — like `git stash` for your environment variables.

> Status: early development. The CLI skeleton is in place; commands are being implemented story by story.

## Build

Requires Go 1.26+.

```sh
make build        # builds a static binary ./envault (CGO disabled, stripped)
# or:
CGO_ENABLED=0 go build -o envault ./cmd/envault
```

## Run

```sh
./envault --help            # list commands
./envault save              # (coming soon) save the current folder's .env into the vault
./envault load              # (coming soon) restore a snapshot into the current folder
./envault                   # (coming soon) open the interactive browser
```

Global flags: `--project <name>` overrides the inferred project, `--stage <name>` selects the stage (when omitted, the stage is resolved to `default`).

## License

MIT — see [LICENSE](LICENSE).
