# envault

Save and restore `.env` files with a single command — like `git stash` for your environment variables.

## Install

**macOS / Linux:**

```sh
curl -fsSL https://raw.githubusercontent.com/fmilioni/envault/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/fmilioni/envault/main/install.ps1 | iex
```

The installer downloads the right binary from the latest [GitHub Release](https://github.com/fmilioni/envault/releases) and verifies its SHA256 checksum. On Windows it adds `envault` to your user PATH; on macOS/Linux it installs to a PATH directory when possible (`/usr/local/bin`), otherwise to `~/.local/bin` and prints the line to add it. Set `ENVAULT_VERSION` to pin a specific tag. Re-running updates an existing install in place.

## Build

Requires Go 1.26+.

```sh
make build        # builds a static binary ./envault (CGO disabled, stripped, trimpath)
make test         # runs the test suite
# or:
CGO_ENABLED=0 go build -o envault ./cmd/envault
```

## Run

```sh
./envault --help            # list commands
./envault --version         # print the version
./envault save              # save the current folder's .env into the vault
./envault load              # restore a snapshot into the current folder
./envault export            # pack projects/stages into a portable bundle
./envault import <bundle>   # import a bundle into the vault
./envault delete            # delete a snapshot or a whole project from the vault
./envault                   # open the interactive browser (no args)
```

Global flags: `--project <name>` overrides the inferred project, `--stage <name>` selects the stage (when omitted, the stage is resolved to `default`).

## Releases

`make release` cross-compiles every supported platform into `dist/` (small static binaries, ~4 MB each) plus a `checksums.txt`:

```sh
make release      # darwin/linux/windows × amd64/arm64 → dist/envault_<os>_<arch>[.exe]
```

Pushing a `v*` tag triggers the GitHub Actions release workflow: it smoke-tests on Linux/macOS/Windows, then builds the matrix and publishes the binaries and checksums to a GitHub Release.

## License

MIT — see [LICENSE](LICENSE).
