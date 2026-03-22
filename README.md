# larkm2ctl

Command-line tool for controlling the [Hollyland Lark M2](https://www.hollyland.com/product/lark-m2) wireless microphone on Linux via USB.

Communicates directly with the receiver (RX) over HID, letting you query status, adjust gain, toggle noise cancellation, and more - no official app required.

## Features

- Real-time transmitter status (connection, battery level)
- Microphone gain control (levels 0–5)
- Noise cancellation on/off and strength (weak/strong)
- Firmware version and serial number queries
- Live monitoring mode
- Device reboot
- Raw command interface for protocol debugging

## Requirements

- Linux with `/dev/hidraw*` support
- Go 1.24+ (to build from source)
- Appropriate permissions to access HID devices (see [Permissions](#permissions))

## Building

```sh
go build -o larkm2ctl
```

## Permissions

The tool accesses `/dev/hidraw*` devices, which typically require root or a udev rule. To use without root, create a udev rule:

```sh
sudo tee /etc/udev/rules.d/99-larkm2.rules <<'EOF'
SUBSYSTEM=="hidraw", ATTRS{idVendor}=="3547", ATTRS{idProduct}=="0007", MODE="0666"
EOF
sudo udevadm control --reload-rules
sudo udevadm trigger
```

Then reconnect the device.

## Usage

```
larkm2ctl <command> [args...]

larkm2ctl status              # Full device status (TX connection, battery, gain, noise, SN, FW)
larkm2ctl monitor             # Continuously poll status every 3s (clears screen)

larkm2ctl version              # Firmware version for RX, TX1, TX2
larkm2ctl version tx1          # Firmware version for TX1 only
larkm2ctl sn                   # Device serial number

larkm2ctl gain                 # Get current gain level
larkm2ctl gain 3               # Set gain level (0=quietest, 5=loudest)

larkm2ctl noise                # Get NC status and strength
larkm2ctl noise on             # Enable noise cancellation
larkm2ctl noise off            # Disable noise cancellation
larkm2ctl noise strong         # Set NC strength to strong
larkm2ctl noise weak           # Set NC strength to weak

larkm2ctl reboot               # Reboot the device

larkm2ctl raw 10               # Send raw command (hex), show response
larkm2ctl dump                 # Send heartbeat, show raw response bytes
larkm2ctl sniff                # Listen for unsolicited device messages
larkm2ctl probe                # Test HID communication
larkm2ctl list                 # List matching hidraw devices
```

## Example Output

```
$ larkm2ctl status
TX1:    connected (89%)
TX2:    disconnected
Gain:   3/5
Noise:  ON (Strong)
SN:     XXXXXXXXXXXX
RX FW:  2.0.0.33
TX1 FW: 2.1.0.33
TX2 FW: 0.0.0.0
```

## How It Works

The tool discovers the Lark M2 receiver by scanning `/dev/hidraw*` devices for USB VID `3547` / PID `0007`. It communicates using a binary protocol (64-byte HID reports).

## Project Structure

```
main.go       CLI entry point, command handlers, output formatting
device.go     HID device discovery, open, read/write, get/set methods
protocol.go   Packet encoding/decoding, constants, data structures
```
