package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Device wraps a hidraw connection to the Lark M2.
type Device struct {
	f  *os.File
	fd int
}

// findHidrawDevices returns all /dev/hidrawN paths matching our VID:PID.
func findHidrawDevices() ([]string, error) {
	target := fmt.Sprintf("0003:%08X:%08X", VID, PID)

	matches, err := filepath.Glob("/sys/class/hidraw/hidraw*/device/uevent")
	if err != nil {
		return nil, err
	}

	var devices []string
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "HID_ID=") && strings.Contains(line, target) {
				name := filepath.Base(filepath.Dir(filepath.Dir(path)))
				devices = append(devices, "/dev/"+name)
			}
		}
	}

	if len(devices) == 0 {
		return nil, fmt.Errorf("Lark M2 not found (VID=%04X PID=%04X) — is it plugged in via USB?", VID, PID)
	}
	return devices, nil
}

// pollRead waits for data on fd with timeout in milliseconds. Returns true if data is ready.
func pollRead(fd int, timeoutMs int) (bool, error) {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, timeoutMs)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Open finds and opens the Lark M2 hidraw device.
// Tries all matching hidraw devices and picks the one that responds.
func Open() (*Device, error) {
	devices, err := findHidrawDevices()
	if err != nil {
		return nil, err
	}

	// Try each device - send a heartbeat and see which responds
	testPkt := BuildPacket(CmdGetHeartInfo, DevTypeRX, nil)
	writeBuf := make([]byte, PacketSize)
	writeBuf[0] = HIDReportID
	copy(writeBuf[1:], testPkt[:HIDReportLen])

	for _, devPath := range devices {
		f, err := os.OpenFile(devPath, os.O_RDWR, 0)
		if err != nil {
			continue
		}
		fd := int(f.Fd())

		_, err = f.Write(writeBuf)
		if err != nil {
			f.Close()
			continue
		}

		ready, err := pollRead(fd, 500)
		if err != nil || !ready {
			f.Close()
			continue
		}

		buf := make([]byte, PacketSize)
		n, err := f.Read(buf)
		if err != nil || n < 2 {
			f.Close()
			continue
		}

		// Check: report ID 0x55, then BB DD header
		if buf[0] == HIDReportID && n > 2 && buf[1] == RespSyncByte1 && buf[2] == SyncByte2 {
			return &Device{f: f, fd: fd}, nil
		}

		f.Close()
	}

	// If none responded, open the first one anyway (for debugging)
	f, err := os.OpenFile(devices[0], os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", devices[0], err)
	}

	return &Device{f: f, fd: int(f.Fd())}, nil
}

// Close releases the device.
func (d *Device) Close() {
	d.f.Close()
}

// SendCommand sends a V1 command and reads the response via hidraw.
// Prepends report ID 0x55 on write, strips it from read.
func (d *Device) SendCommand(cmd byte, devType byte, payload []byte) ([]byte, error) {
	pkt := BuildPacket(cmd, devType, payload)

	// Hidraw write: [report_id] + [protocol data (63 bytes)]
	writeBuf := make([]byte, PacketSize)
	writeBuf[0] = HIDReportID
	copy(writeBuf[1:], pkt[:HIDReportLen])

	_, err := d.f.Write(writeBuf)
	if err != nil {
		return nil, fmt.Errorf("hidraw write: %w", err)
	}

	// Wait for response with timeout
	ready, err := pollRead(d.fd, 2000)
	if err != nil {
		return nil, fmt.Errorf("poll: %w", err)
	}
	if !ready {
		return nil, fmt.Errorf("no response from device (timeout)")
	}

	buf := make([]byte, PacketSize)
	n, err := d.f.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("hidraw read: %w", err)
	}

	// Strip report ID prefix
	if n > 0 && buf[0] == HIDReportID {
		return buf[1:n], nil
	}
	return buf[:n], nil
}

// SendCommandWithReportID sends with a report ID byte prepended (for HID devices that use report IDs).
func (d *Device) SendCommandWithReportID(reportID byte, cmd byte, devType byte, payload []byte) ([]byte, error) {
	pkt := BuildPacket(cmd, devType, payload)
	withID := make([]byte, len(pkt)+1)
	withID[0] = reportID
	copy(withID[1:], pkt)

	_, err := d.f.Write(withID)
	if err != nil {
		return nil, fmt.Errorf("hidraw write: %w", err)
	}

	ready, err := pollRead(d.fd, 2000)
	if err != nil {
		return nil, fmt.Errorf("poll: %w", err)
	}
	if !ready {
		return nil, fmt.Errorf("no response from device (timeout)")
	}

	buf := make([]byte, PacketSize+1)
	n, err := d.f.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("hidraw read: %w", err)
	}

	return buf[:n], nil
}

// command sends a command to device type RX and parses the response payload.
func (d *Device) command(cmd byte, payload []byte) ([]byte, error) {
	return d.commandTo(cmd, DevTypeRX, payload)
}

// commandTo sends a command to a specific device type and parses the response payload.
func (d *Device) commandTo(cmd byte, devType byte, payload []byte) ([]byte, error) {
	resp, err := d.SendCommand(cmd, devType, payload)
	if err != nil {
		return nil, err
	}
	_, _, p, err := ParseResponse(resp)
	return p, err
}

// --- GET commands ---

func (d *Device) GetHeartInfo() (HeartInfo, error) {
	p, err := d.command(CmdGetHeartInfo, nil)
	if err != nil {
		return HeartInfo{}, err
	}
	return ParseHeartInfo(p), nil
}

func (d *Device) GetVoiceLevel() (int, error) {
	p, err := d.command(CmdGetVoiceLevel, nil)
	if err != nil {
		return 0, err
	}
	if len(p) < 1 {
		return 0, fmt.Errorf("empty voice level response")
	}
	return int(p[0]), nil
}

func (d *Device) GetNoise() (int, error) {
	p, err := d.command(CmdGetNoise, nil)
	if err != nil {
		return 0, err
	}
	if len(p) < 1 {
		return 0, fmt.Errorf("empty noise response")
	}
	return int(p[0]), nil
}

func (d *Device) GetSpeaker() (bool, error) {
	p, err := d.command(CmdGetSpeaker, nil)
	if err != nil {
		return false, err
	}
	if len(p) < 1 {
		return false, fmt.Errorf("empty speaker response")
	}
	return p[0] == 0, nil
}

func (d *Device) GetVoiceLock() (int, error) {
	p, err := d.command(CmdGetVoiceLock, nil)
	if err != nil {
		return 0, err
	}
	if len(p) < 1 {
		return 0, fmt.Errorf("empty voice lock response")
	}
	return int(p[0]), nil
}

func (d *Device) GetSN() (string, error) {
	p, err := d.command(CmdGetSN, nil)
	if err != nil {
		return "", err
	}
	return FormatSN(p), nil
}

func (d *Device) GetVersion(devType byte) (string, error) {
	p, err := d.commandTo(CmdGetVersion, devType, nil)
	if err != nil {
		return "", err
	}
	return FormatVersion(p), nil
}

func (d *Device) GetBtnCustom() (int, int, error) {
	p, err := d.command(CmdGetBtnCustom, nil)
	if err != nil {
		return 0, 0, err
	}
	if len(p) < 2 {
		return 0, 0, fmt.Errorf("button custom response too short")
	}
	return int(p[0]), int(p[1]), nil
}

// --- SET commands ---

// setCmd sends a SET command. Doesn't fail if the device doesn't respond
// (some SET commands are fire-and-forget on certain firmware versions).
func (d *Device) setCmd(cmd byte, payload []byte) error {
	pkt := BuildPacket(cmd, DevTypeRX, payload)

	writeBuf := make([]byte, PacketSize)
	writeBuf[0] = HIDReportID
	copy(writeBuf[1:], pkt[:HIDReportLen])

	_, err := d.f.Write(writeBuf)
	if err != nil {
		return fmt.Errorf("hidraw write: %w", err)
	}

	// Try to read response, but don't fail on timeout
	ready, _ := pollRead(d.fd, 500)
	if ready {
		buf := make([]byte, PacketSize)
		d.f.Read(buf)
	}
	return nil
}

func (d *Device) SetVoiceLevel(level int) error {
	return d.setCmd(CmdSetVoiceLevel, []byte{byte(level)})
}

func (d *Device) SetNoise(level int) error {
	return d.setCmd(CmdSetNoise, []byte{byte(level)})
}

func (d *Device) SetNoiseStatus(value byte) error {
	return d.setCmd(CmdSetNoiseStatus, []byte{value})
}

func (d *Device) SetVoiceMode(mode int) error {
	return d.setCmd(CmdSetVoiceMode, []byte{byte(mode)})
}

func (d *Device) SetSpeaker(value int) error {
	return d.setCmd(CmdSetSpeaker, []byte{byte(value)})
}

func (d *Device) SetLight(value int) error {
	return d.setCmd(CmdSetLight, []byte{byte(value)})
}

func (d *Device) SetVoiceLock(value int) error {
	return d.setCmd(CmdSetVoiceLock, []byte{byte(value)})
}

func (d *Device) SetShutdownTime(value int) error {
	return d.setCmd(CmdSetShutdownTime, []byte{byte(value)})
}

func (d *Device) Reboot() error {
	_, err := d.command(CmdRestart, nil)
	return err
}

func (d *Device) SendRaw(cmd byte, devType byte, payload []byte) ([]byte, error) {
	return d.SendCommand(cmd, devType, payload)
}

// hidrawReportDescriptor matches the kernel struct hidraw_report_descriptor.
type hidrawReportDescriptor struct {
	Size  uint32
	Value [4096]byte
}

// readHIDDescriptor reads the raw HID report descriptor via ioctl.
func (d *Device) readHIDDescriptor() ([]byte, error) {
	// Get descriptor size
	var descSize int32
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd),
		uintptr(0x80044801), // HIDIOCGRDESCSIZE
		uintptr(unsafe.Pointer(&descSize)))
	if errno != 0 {
		return nil, fmt.Errorf("HIDIOCGRDESCSIZE: %v", errno)
	}

	// Get descriptor
	var desc hidrawReportDescriptor
	desc.Size = uint32(descSize)
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd),
		uintptr(0x90044802), // HIDIOCGRDESC
		uintptr(unsafe.Pointer(&desc)))
	if errno != 0 {
		return nil, fmt.Errorf("HIDIOCGRDESC: %v", errno)
	}

	return desc.Value[:descSize], nil
}

// parseHIDDescriptorReportIDs extracts report IDs for input/output/feature reports.
func parseHIDDescriptorReportIDs(desc []byte) (inputIDs, outputIDs, featureIDs []byte) {
	var currentReportID byte
	hasReportID := false
	i := 0
	for i < len(desc) {
		item := desc[i]
		bSize := int(item & 0x03)
		if bSize == 3 {
			bSize = 4
		}
		bType := (item >> 2) & 0x03
		bTag := (item >> 4) & 0x0F

		if i+1+bSize > len(desc) {
			break
		}

		// Report ID (Global item, tag=8, type=1)
		if bType == 1 && bTag == 8 && bSize >= 1 {
			currentReportID = desc[i+1]
			hasReportID = true
		}

		// Main items
		if bType == 0 {
			rid := currentReportID
			if !hasReportID {
				rid = 0
			}
			switch bTag {
			case 8: // Input
				inputIDs = appendUnique(inputIDs, rid)
			case 9: // Output
				outputIDs = appendUnique(outputIDs, rid)
			case 11: // Feature
				featureIDs = appendUnique(featureIDs, rid)
			}
		}

		i += 1 + bSize
	}
	return
}

func appendUnique(s []byte, b byte) []byte {
	for _, v := range s {
		if v == b {
			return s
		}
	}
	return append(s, b)
}

// Probe tries to communicate with the device and prints debug info.
func (d *Device) Probe() {
	fmt.Printf("Device: %s (fd=%d)\n\n", d.f.Name(), d.fd)

	// Read and parse HID descriptor
	desc, err := d.readHIDDescriptor()
	if err != nil {
		fmt.Printf("Error reading HID descriptor: %v\n", err)
	} else {
		fmt.Printf("HID Report Descriptor (%d bytes):\n%s\n", len(desc), HexDump(desc))
		inputIDs, outputIDs, featureIDs := parseHIDDescriptorReportIDs(desc)
		fmt.Printf("Input report IDs:   %v\n", inputIDs)
		fmt.Printf("Output report IDs:  %v\n", outputIDs)
		fmt.Printf("Feature report IDs: %v\n\n", featureIDs)

		// Try writing with each output report ID
		pkt := BuildPacket(CmdGetHeartInfo, DevTypeRX, nil)
		for _, rid := range outputIDs {
			fmt.Printf("--- Trying output report ID 0x%02X ---\n", rid)
			var writeData []byte
			if rid == 0 {
				writeData = pkt
			} else {
				writeData = make([]byte, len(pkt)+1)
				writeData[0] = rid
				copy(writeData[1:], pkt)
			}
			_, werr := d.f.Write(writeData)
			if werr != nil {
				fmt.Printf("Write error: %v\n", werr)
				continue
			}
			fmt.Println("Write OK")
			ready, _ := pollRead(d.fd, 1000)
			if ready {
				buf := make([]byte, 256)
				n, _ := d.f.Read(buf)
				fmt.Printf("Response (%d bytes):\n%s\n", n, HexDump(buf[:n]))
			} else {
				fmt.Println("No response (1s timeout)")
			}
		}

		// If no output report IDs worked, also try feature report IDs
		if len(featureIDs) > 0 {
			fmt.Println("\n--- Trying feature reports via ioctl ---")
			for _, rid := range featureIDs {
				fmt.Printf("Feature report ID 0x%02X: ", rid)
				pktWithID := make([]byte, PacketSize+1)
				pktWithID[0] = rid
				copy(pktWithID[1:], pkt)
				// HIDIOCSFEATURE = _IOC(_IOC_WRITE|_IOC_READ, 'H', 0x06, len)
				iocVal := uintptr(0xC0004806 | (uintptr(len(pktWithID)) << 16))
				_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd), iocVal,
					uintptr(unsafe.Pointer(&pktWithID[0])))
				if errno != 0 {
					fmt.Printf("ioctl error: %v\n", errno)
					continue
				}
				fmt.Println("Write OK")
				ready, _ := pollRead(d.fd, 1000)
				if ready {
					buf := make([]byte, 256)
					n, _ := d.f.Read(buf)
					fmt.Printf("Response (%d bytes):\n%s\n", n, HexDump(buf[:n]))
				} else {
					fmt.Println("No response (1s timeout)")
				}
			}
		}
	}
}

// ListDevices prints all matching hidraw devices for debugging.
func ListDevices() {
	target := fmt.Sprintf("0003:%08X:%08X", VID, PID)
	matches, _ := filepath.Glob("/sys/class/hidraw/hidraw*/device/uevent")
	fmt.Printf("Looking for HID_ID containing: %s\n\n", target)

	for _, path := range matches {
		data, _ := os.ReadFile(path)
		name := filepath.Base(filepath.Dir(filepath.Dir(path)))
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "HID_ID=") {
				match := ""
				if strings.Contains(line, target) {
					match = " <-- MATCH"
				}
				fmt.Printf("/dev/%-10s %s%s\n", name, line, match)
			}
		}
	}
}

// OpenSpecific opens a specific hidraw device path.
func OpenSpecific(devPath string) (*Device, error) {
	f, err := os.OpenFile(devPath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", devPath, err)
	}
	return &Device{f: f, fd: int(f.Fd())}, nil
}

// ReadAny reads whatever is available on the hidraw device (for sniffing).
func (d *Device) ReadAny(timeoutMs int) ([]byte, error) {
	ready, err := pollRead(d.fd, timeoutMs)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, nil
	}
	buf := make([]byte, 256)
	n, err := d.f.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// WriteRaw writes raw bytes to the hidraw device.
func (d *Device) WriteRaw(data []byte) error {
	_, err := d.f.Write(data)
	return err
}

func init() {
	// Prevent "unused import" for time
	_ = time.Second
}
