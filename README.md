# WOL Sender

Lightweight Wake-on-LAN sender with a local browser UI and no third-party runtime dependencies.

The executable binds only to `127.0.0.1`, enumerates active non-loopback IPv4 interfaces on Windows and Linux, and sends the magic packet from the selected interface address to that interface's directed broadcast address.

## Discover devices

Pick an interface and click **Scan network** to sweep that interface's IPv4 subnet. The scan pings each host with the operating system's `ping` command, then reads the OS neighbor/ARP table to attach a MAC address and reverse-DNS name to every host that responded. Select a device to wake it; the app remembers devices (with their MAC) in your browser's `localStorage` only — nothing is persisted server-side.

**Honest limitation:** discovery can only find hosts that are **currently reachable**, because a MAC address is learned from live traffic (ping → ARP). Wake-on-LAN itself needs a MAC, and an already-asleep machine cannot be discovered. The workflow is therefore: scan while the machine is on to learn and save its MAC, then wake it later from the saved entry. A hostname or IP alone cannot wake an offline machine.

Platform notes:

- **Scan cap:** a single scan probes at most 512 hosts. A `/24` or `/23` works; larger subnets are refused with a clear error — scan a smaller range.
- **Ping/ARP differ per OS:** Windows uses `ping -n`/`arp -a`, Linux uses `ping -W`/`/proc/net/arp`, macOS uses `ping -W`(ms)/`arp -a`. Some firewalls drop ICMP, so a live host may still show as unreachable; enter its MAC manually to save it.
- Reverse DNS and MAC resolution are best-effort — a reachable host may appear with no hostname or no MAC.

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
