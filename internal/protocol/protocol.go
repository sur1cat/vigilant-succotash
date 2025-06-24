package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"log"
	"strconv"
)

func xorChecksum(data []byte) byte {
	var chk byte
	for _, b := range data {
		chk ^= b
	}
	return chk
}

func validateChecksum(data []byte) bool {
	if len(data) < 5 {
		return false
	}
	expected := data[4]
	calculated := xorChecksum(data[5:])
	return expected == calculated
}

func CreateCommand(cmd string, tokenHex string, slotStr string) []byte {
	token, err := hex.DecodeString(tokenHex)
	if err != nil || len(token) != 4 {
		log.Printf("Invalid token: %v", err)
		return nil
	}

	buf := &bytes.Buffer{}
	cmdByte := byte(0x00)
	var payload []byte
	var packLen uint16 = 7 // Минимальная длина: PackLen(2) + Cmd(1) + Version(1) + CheckSum(1) + Token(4)

	switch cmd {
	case "heartbeat":
		cmdByte = 0x61
	case "query_fw":
		cmdByte = 0x62
	case "restart":
		cmdByte = 0x67
	case "query_iccid":
		cmdByte = 0x69
	case "voice_get":
		cmdByte = 0x77
	case "rent":
		cmdByte = 0x65
		if slotInt, err := strconv.Atoi(slotStr); err == nil && slotInt >= 1 && slotInt <= 255 {
			payload = []byte{byte(slotInt)}
			packLen++
		} else {
			log.Printf("Invalid slot for rent: %s", slotStr)
			return nil
		}
	case "eject":
		cmdByte = 0x80
		if slotInt, err := strconv.Atoi(slotStr); err == nil && slotInt >= 1 && slotInt <= 255 {
			payload = []byte{byte(slotInt)}
			packLen++
		} else {
			log.Printf("Invalid slot for eject: %s", slotStr)
			return nil
		}
	case "query_power_bank":
		cmdByte = 0x64
	case "voice_set":
		cmdByte = 0x70
		if level, err := strconv.Atoi(slotStr); err == nil && level >= 0 && level <= 15 {
			payload = []byte{byte(level)}
			packLen++
		} else {
			log.Printf("Invalid voice level: %s", slotStr)
			return nil
		}
	default:
		log.Printf("Unknown command: %s", cmd)
		return nil
	}

	binary.Write(buf, binary.BigEndian, packLen)
	buf.WriteByte(cmdByte)
	buf.WriteByte(0x01) // Version
	buf.WriteByte(0x00) // Placeholder for CheckSum
	buf.Write(token)
	if len(payload) > 0 {
		buf.Write(payload)
	}

	data := buf.Bytes()
	data[4] = xorChecksum(data[5:]) // Calculate CheckSum over payload and token

	return data
}

func HandleIncoming(data []byte) ([]byte, string) {
	if len(data) < 7 || !validateChecksum(data) {
		log.Println("Invalid packet or checksum")
		return nil, ""
	}

	cmd := data[2]
	token := data[5:9]
	var stationID string

	switch cmd {
	case 0x60: // Login
		log.Println("Received login")
		if len(data) >= 23 { // Минимальная длина для Login с BoxID
			boxIDLen := binary.BigEndian.Uint16(data[15:17])
			if len(data) >= int(17+boxIDLen) {
				stationID = string(data[17 : 17+boxIDLen-1]) // -1 to exclude null terminator
			}
		}
		resp := bytes.NewBuffer(nil)
		binary.Write(resp, binary.BigEndian, uint16(8))
		resp.Write([]byte{0x60, 0x01, 0x01}) // Cmd, Version, CheckSum (placeholder)
		resp.Write(token)
		resp.WriteByte(0x01) // Success
		respBytes := resp.Bytes()
		respBytes[4] = xorChecksum(respBytes[5:])
		return respBytes, stationID

	case 0x61: // Heartbeat
		log.Println("Received heartbeat")
		return data[:7], ""

	case 0x66: // Return Power Bank
		if len(data) >= 18 {
			slot := data[9]
			powerBankID := data[10:18]
			log.Printf("Received Return Power Bank from slot %d, ID: %x", slot, powerBankID)
			resp := bytes.NewBuffer(nil)
			binary.Write(resp, binary.BigEndian, uint16(9))
			resp.Write([]byte{0x66, 0x01, 0x00}) // Cmd, Version, CheckSum
			resp.Write(token)
			resp.WriteByte(slot)
			resp.WriteByte(0x01) // Success
			respBytes := resp.Bytes()
			respBytes[4] = xorChecksum(respBytes[5:])
			return respBytes, ""
		}

	case 0x62: // Query Firmware Version
		log.Println("Received query firmware version")
		fwVersion := "RL1,H6,08,14" // Example version
		fwLen := uint16(len(fwVersion) + 1)
		resp := bytes.NewBuffer(nil)
		binary.Write(resp, binary.BigEndian, uint16(7+2+fwLen))
		resp.Write([]byte{0x62, 0x01, 0x00}) // Cmd, Version, CheckSum
		resp.Write(token)
		binary.Write(resp, binary.BigEndian, fwLen)
		resp.WriteString(fwVersion)
		resp.WriteByte(0x00) // Null terminator
		respBytes := resp.Bytes()
		respBytes[4] = xorChecksum(respBytes[5:])
		return respBytes, ""

	case 0x65: // Rent Power Bank
		if len(data) >= 10 {
			slot := data[9]
			log.Printf("Received rent power bank response for slot %d", slot)
			powerBankID := []byte("RL1A|000d") // Example ID
			resp := bytes.NewBuffer(nil)
			binary.Write(resp, binary.BigEndian, uint16(17))
			resp.Write([]byte{0x65, 0x01, 0x00}) // Cmd, Version, CheckSum
			resp.Write(token)
			resp.WriteByte(slot)
			resp.WriteByte(0x01) // Success
			resp.Write(powerBankID)
			respBytes := resp.Bytes()
			respBytes[4] = xorChecksum(respBytes[5:])
			return respBytes, ""
		}

	case 0x80: // Eject Power Bank
		if len(data) >= 10 {
			slot := data[9]
			log.Printf("Received eject power bank response for slot %d", slot)
			powerBankID := []byte("RL1A|000d") // Example ID
			resp := bytes.NewBuffer(nil)
			binary.Write(resp, binary.BigEndian, uint16(17))
			resp.Write([]byte{0x80, 0x01, 0x00}) // Cmd, Version, CheckSum
			resp.Write(token)
			resp.WriteByte(slot)
			resp.WriteByte(0x01) // Success
			resp.Write(powerBankID)
			respBytes := resp.Bytes()
			respBytes[4] = xorChecksum(respBytes[5:])
			return respBytes, ""
		}

	case 0x69: // Query ICCID
		log.Println("Received query ICCID")
		iccid := "89860416121880245965" // Example ICCID
		iccidLen := uint16(len(iccid) + 1)
		resp := bytes.NewBuffer(nil)
		binary.Write(resp, binary.BigEndian, uint16(7+2+iccidLen))
		resp.Write([]byte{0x69, 0x01, 0x00}) // Cmd, Version, CheckSum
		resp.Write(token)
		binary.Write(resp, binary.BigEndian, iccidLen)
		resp.WriteString(iccid)
		resp.WriteByte(0x00) // Null terminator
		respBytes := resp.Bytes()
		respBytes[4] = xorChecksum(respBytes[5:])
		return respBytes, ""

	case 0x77: // Get Voice Level
		log.Println("Received get voice level")
		resp := bytes.NewBuffer(nil)
		binary.Write(resp, binary.BigEndian, uint16(8))
		resp.Write([]byte{0x77, 0x01, 0x00}) // Cmd, Version, CheckSum
		resp.Write(token)
		resp.WriteByte(0x0e) // Example level
		respBytes := resp.Bytes()
		respBytes[4] = xorChecksum(respBytes[5:])
		return respBytes, ""

	default:
		log.Printf("Received unhandled cmd: 0x%02x", cmd)
	}

	return nil, ""
}
