package main

import "fmt"

// USB identifiers
const (
	VID          = 0x3547
	PID          = 0x0007
	PacketSize   = 64 // protocol packet size
	HIDReportID  = 0x55
	HIDReportLen = 63 // HID report data size (PacketSize - 1 for report ID)
)

// Protocol V1 markers
const (
	SyncByte1     = 0xAA
	SyncByte2     = 0xDD
	RespSyncByte1 = 0xBB // Response uses BB DD, request uses AA DD
)

// Device type bytes for outgoing requests
const (
	DevTypeRX  = 0x40
	DevTypeTX1 = 0x41
	DevTypeTX2 = 0x42
)

// Device type bytes in responses (high bit set)
const (
	RespTypeRX  = 0x80
	RespTypeTX1 = 0x81
	RespTypeTX2 = 0x82
)

// Protocol V1 command codes
const (
	CmdGetVersion      = 0x03
	CmdGetVoiceLevel   = 0x04
	CmdSetVoiceLevel   = 0x05
	CmdSetNoise        = 0x06
	CmdGetDeviceID     = 0x08
	CmdUpgradeResponse = 0x0B
	CmdRestart         = 0x0C
	CmdRXUpgrade       = 0x0D
	CmdGetHeartInfo    = 0x10
	CmdGetNoise        = 0x11
	CmdSetSpeaker      = 0x12
	CmdGetSpeaker      = 0x13
	CmdGetSN           = 0x15
	CmdSetVoiceMode    = 0x18
	CmdSetNoiseStatus  = 0x19
	CmdSetVoiceLock    = 0x34
	CmdGetVoiceLock    = 0x35
	CmdSetShutdownTime = 0x36
	CmdSetBtnCustom    = 0x37
	CmdGetBtnCustom    = 0x38
	CmdSetLight        = 0x39
)

// Logical device types for the API
const (
	DeviceRX  = 0
	DeviceTX1 = 1
	DeviceTX2 = 2
)

// BuildPacket constructs a V1 protocol packet.
// Format: [AA DD CMD DEVTYPE LEN_H LEN_L PAYLOAD...] padded to 64 bytes
func BuildPacket(cmd byte, devType byte, payload []byte) []byte {
	pkt := make([]byte, PacketSize)
	pkt[0] = SyncByte1
	pkt[1] = SyncByte2
	pkt[2] = cmd
	pkt[3] = devType
	pktLen := len(payload)
	pkt[4] = byte((pktLen >> 8) & 0xFF)
	pkt[5] = byte(pktLen & 0xFF)
	copy(pkt[6:], payload)
	return pkt
}

// ParseResponse extracts the command, device type, and payload from a response.
func ParseResponse(data []byte) (cmd byte, devType byte, payload []byte, err error) {
	if len(data) < 6 {
		return 0, 0, nil, fmt.Errorf("response too short: %d bytes", len(data))
	}
	if data[0] != RespSyncByte1 || data[1] != SyncByte2 {
		return 0, 0, nil, fmt.Errorf("invalid response header: %02X %02X (expected BB DD)", data[0], data[1])
	}
	cmd = data[2]
	devType = data[3]
	// Non-standard length encoding: high * 16 + low
	lenHi := int(data[4] & 0xFF)
	lenLo := int(data[5] & 0xFF)
	payloadLen := lenHi*16 + lenLo
	if 6+payloadLen > len(data) {
		payloadLen = len(data) - 6
	}
	payload = data[6 : 6+payloadLen]
	return cmd, devType, payload, nil
}

// HeartInfo holds the device status from a V1 heartbeat response.
type HeartInfo struct {
	PayloadLen   int
	TX1Connected bool
	TX2Connected bool
	TX1Battery   int
	TX2Battery   int
	UVValue      int
	NoiseLevel   int
	VoiceLevel   int
	VoiceLock    int
	// Fields below only present if payload > 9 bytes
	VoiceMode    int
	ShutdownTime int
	BtnCustom1   int
	BtnCustom2   int
	LightOn      bool
}

// ParseHeartInfo parses a V1 heartbeat payload (typically 9 bytes on Lark M2).
func ParseHeartInfo(payload []byte) HeartInfo {
	h := HeartInfo{PayloadLen: len(payload)}
	get := func(i int) int {
		if i < len(payload) {
			return int(payload[i])
		}
		return 0
	}
	h.TX1Connected = get(0) == 1
	h.TX2Connected = get(1) == 1
	h.TX1Battery = get(2)
	h.TX2Battery = get(3)
	if len(payload) > 5 {
		h.UVValue = int(payload[4])*256 + int(payload[5])
	}
	h.NoiseLevel = get(6)
	h.VoiceLevel = get(7)
	h.VoiceLock = get(8)
	if len(payload) > 9 {
		h.VoiceMode = get(9)
	}
	if len(payload) > 10 {
		h.ShutdownTime = get(10)
	}
	if len(payload) > 12 {
		h.BtnCustom1 = get(11)
		h.BtnCustom2 = get(12)
	}
	if len(payload) > 13 {
		h.LightOn = payload[13] == 0
	}
	return h
}

func VoiceModeString(mode int) string {
	switch mode {
	case 0:
		return "Mono"
	case 1:
		return "Stereo"
	default:
		return fmt.Sprintf("Unknown(%d)", mode)
	}
}

func OnOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func FormatVersion(data []byte) string {
	if len(data) == 0 {
		return "unknown"
	}
	s := ""
	for i, b := range data {
		if i > 0 {
			s += "."
		}
		s += fmt.Sprintf("%d", b)
	}
	return s
}

func FormatSN(data []byte) string {
	s := ""
	for _, b := range data {
		if b != 0x00 && b != 0xFF {
			s += string(rune(b))
		}
	}
	return s
}

func HexDump(data []byte) string {
	s := ""
	for i, b := range data {
		s += fmt.Sprintf("%02X ", b)
		if (i+1)%16 == 0 {
			s += "\n"
		}
	}
	return s
}
