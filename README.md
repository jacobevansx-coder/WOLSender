# WOL Sender

Lightweight Wake-on-LAN sender with a local browser UI and no third-party runtime dependencies.

The executable binds only to `127.0.0.1`, enumerates active non-loopback IPv4 interfaces on Windows and Linux, and sends the magic packet from the selected interface address to that interface's directed broadcast address.

## Build

Install Go 1.22 or newer, then build on the target platform:

```powershell
go test ./...
go build -trimpath -ldflags="-s -w" -o dist\wolsender.exe .
```

Linux:

```sh
go test ./...
go build -trimpath -ldflags="-s -w" -o dist/wolsender .
```

Cross-compile from Windows:

```powershell
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o dist\wolsender-linux-amd64 .
Remove-Item Env:GOOS, Env:GOARCH
```

## Run

```powershell
.\dist\wolsender.exe
```

The app opens a browser window automatically. To keep it headless:

```powershell
.\dist\wolsender.exe -no-browser
```

Optional flags:

- `-host`: HTTP bind host, defaults to `127.0.0.1`
- `-port`: HTTP bind port, defaults to `0` for an available port
- `-no-browser`: print the URL without opening a browser

## Notes

Wake-on-LAN usually requires the target machine, BIOS/UEFI, operating system, NIC, and switch path to allow WoL. Some devices listen on UDP port `7`; the default is `9`.
