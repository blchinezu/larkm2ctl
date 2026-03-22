package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func usage() {
	fmt.Fprintf(os.Stderr, `larkm2ctl — Hollyland Lark M2 USB control tool

Usage: larkm2ctl <command> [args...]

Status:
  status                Full device status
  monitor               Continuously poll status every 3s

Info:
  version [rx|tx1|tx2]  Firmware version (default: all)
  sn                    Device serial number

Audio:
  gain                  Get gain level (0-5)
  gain <0-5>            Set gain level (0=quietest, 5=loudest)

Noise Cancellation:
  noise                 Get NC status and strength
  noise <on|off>        Enable/disable noise cancellation
  noise <strong|weak>   Set NC strength

Other:
  reboot                Reboot the device
  raw <cmd> [bytes..]   Send raw command (hex)
  dump                  Send heartbeat, show raw response
  probe                 Debug: test HID communication
  list                  Debug: list hidraw devices
`)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	if cmd == "list" {
		ListDevices()
		return
	}

	dev, err := Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer dev.Close()

	switch cmd {
	case "status":
		cmdStatus(dev)
	case "monitor":
		cmdMonitor(dev)
	case "version":
		cmdVersion(dev, args)
	case "sn":
		cmdSN(dev)
	case "gain":
		cmdGain(dev, args)
	case "noise":
		cmdNoise(dev, args)
	case "reboot":
		fatal(dev.Reboot())
		fmt.Println("Device rebooting")
	case "raw":
		cmdRaw(dev, args)
	case "dump":
		cmdDump(dev)
	case "probe":
		dev.Probe()
	case "sniff":
		cmdSniff(dev)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		usage()
	}
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func mustInt(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid number: %s\n", s)
		os.Exit(1)
	}
	return v
}

func devTypeByte(s string) byte {
	switch strings.ToLower(s) {
	case "rx":
		return DevTypeRX
	case "tx1":
		return DevTypeTX1
	case "tx2":
		return DevTypeTX2
	default:
		fmt.Fprintf(os.Stderr, "Invalid device type: %s (use rx, tx1, tx2)\n", s)
		os.Exit(1)
		return 0
	}
}

var noiseLevelNames = map[int]string{1: "Weak", 2: "Strong"}

func printStatus(h HeartInfo) {
	for _, mic := range []struct {
		name      string
		connected bool
		battery   int
	}{
		{"TX1", h.TX1Connected, h.TX1Battery},
		{"TX2", h.TX2Connected, h.TX2Battery},
	} {
		if mic.connected {
			fmt.Printf("%-7s connected (%d%%)\n", mic.name+":", mic.battery)
		} else {
			fmt.Printf("%-7s disconnected\n", mic.name+":")
		}
	}

	fmt.Printf("Gain:   %d/5\n", h.VoiceLevel)

	if h.NoiseLevel == 0 {
		fmt.Println("Noise:  OFF")
	} else {
		name := noiseLevelNames[h.NoiseLevel]
		if name == "" {
			name = fmt.Sprintf("Level %d", h.NoiseLevel)
		}
		fmt.Printf("Noise:  ON (%s)\n", name)
	}
}

func cmdStatus(dev *Device) {
	h, err := dev.GetHeartInfo()
	fatal(err)
	printStatus(h)

	sn, err := dev.GetSN()
	if err == nil {
		fmt.Printf("SN:     %s\n", sn)
	}
	for _, t := range []struct {
		name string
		dt   byte
	}{
		{"RX", DevTypeRX}, {"TX1", DevTypeTX1}, {"TX2", DevTypeTX2},
	} {
		v, err := dev.GetVersion(t.dt)
		if err == nil {
			fmt.Printf("%-8s%s\n", t.name+" FW:", v)
		}
	}
}

func cmdMonitor(dev *Device) {
	for {
		h, err := dev.GetHeartInfo()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		} else {
			fmt.Print("\033[2J\033[H")
			fmt.Printf("[%s]\n", time.Now().Format("15:04:05"))
			printStatus(h)
		}
		time.Sleep(3 * time.Second)
	}
}

func cmdVersion(dev *Device, args []string) {
	types := []struct {
		name string
		dt   byte
	}{
		{"RX", DevTypeRX}, {"TX1", DevTypeTX1}, {"TX2", DevTypeTX2},
	}

	if len(args) > 0 {
		dt := devTypeByte(args[0])
		v, err := dev.GetVersion(dt)
		fatal(err)
		fmt.Printf("%s: %s\n", strings.ToUpper(args[0]), v)
		return
	}

	for _, t := range types {
		v, err := dev.GetVersion(t.dt)
		if err != nil {
			fmt.Printf("%s: error (%v)\n", t.name, err)
		} else {
			fmt.Printf("%s: %s\n", t.name, v)
		}
	}
}

func cmdSN(dev *Device) {
	sn, err := dev.GetSN()
	fatal(err)
	fmt.Printf("SN: %s\n", sn)
}

func cmdGain(dev *Device, args []string) {
	if len(args) == 0 {
		level, err := dev.GetVoiceLevel()
		fatal(err)
		fmt.Printf("Gain: %d (0=quietest, 5=loudest)\n", level)
		return
	}
	level := mustInt(args[0])
	if level < 0 || level > 5 {
		fmt.Fprintf(os.Stderr, "Gain must be 0-5 (0=quietest, 5=loudest)\n")
		os.Exit(1)
	}
	fatal(dev.SetVoiceLevel(level))
	fmt.Printf("Gain set to %d\n", level)
}

func cmdNoise(dev *Device, args []string) {
	if len(args) == 0 {
		h, err := dev.GetHeartInfo()
		fatal(err)
		if h.NoiseLevel == 0 {
			fmt.Println("Noise cancellation: OFF")
		} else {
			name := noiseLevelNames[h.NoiseLevel]
			if name == "" {
				name = fmt.Sprintf("Level %d", h.NoiseLevel)
			}
			fmt.Printf("Noise cancellation: ON (%s)\n", name)
		}
		return
	}
	switch strings.ToLower(args[0]) {
	case "on":
		fatal(dev.SetNoiseStatus(1))
		fmt.Println("Noise cancellation ON")
	case "off":
		fatal(dev.SetNoiseStatus(0))
		fmt.Println("Noise cancellation OFF")
	case "weak", "1":
		fatal(dev.SetNoise(1))
		fmt.Println("Noise cancellation: Weak")
	case "strong", "2":
		fatal(dev.SetNoise(2))
		fmt.Println("Noise cancellation: Strong")
	default:
		fmt.Fprintf(os.Stderr, "Use: noise <on|off|strong|weak>\n")
		os.Exit(1)
	}
}

func cmdDump(dev *Device) {
	fmt.Println("Sending heartbeat (cmd 0x10) to RX...")
	resp, err := dev.SendCommand(CmdGetHeartInfo, DevTypeRX, nil)
	fatal(err)
	fmt.Printf("Response (%d bytes):\n%s\n", len(resp), HexDump(resp))
	_, dt, p, err := ParseResponse(resp)
	if err != nil {
		fmt.Printf("Parse error: %v\n", err)
		return
	}
	fmt.Printf("Device type: 0x%02X\n", dt)
	fmt.Printf("Payload (%d bytes): %s\n", len(p), HexDump(p))
}

func cmdSniff(dev *Device) {
	fmt.Println("Listening for data on hidraw (Ctrl+C to stop)...")
	for {
		data, err := dev.ReadAny(3000)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}
		if data == nil {
			fmt.Print(".")
			continue
		}
		fmt.Printf("\n[%s] %d bytes:\n%s", time.Now().Format("15:04:05.000"), len(data), HexDump(data))
	}
}

func cmdRaw(dev *Device, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: raw <cmd_hex> [payload_hex_bytes...]\n")
		fmt.Fprintf(os.Stderr, "Example: raw 10  (heartbeat)\n")
		os.Exit(1)
	}
	cmdByte, err := strconv.ParseUint(args[0], 16, 8)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid command byte: %s\n", args[0])
		os.Exit(1)
	}
	var payload []byte
	for _, a := range args[1:] {
		b, err := strconv.ParseUint(a, 16, 8)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid hex byte: %s\n", a)
			os.Exit(1)
		}
		payload = append(payload, byte(b))
	}

	resp, err := dev.SendCommand(byte(cmdByte), DevTypeRX, payload)
	fatal(err)

	fmt.Printf("Response (%d bytes):\n%s\n", len(resp), HexDump(resp))

	_, dt, p, err := ParseResponse(resp)
	if err == nil {
		fmt.Printf("Device type: 0x%02X\n", dt)
		fmt.Printf("Parsed payload (%d bytes): %v\n", len(p), p)
	} else {
		fmt.Printf("Parse error: %v\n", err)
	}
}
