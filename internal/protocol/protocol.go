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

func CreateCommand(cmd string, tokenHex string, slotStr string) []byte {
	token, err := hex.DecodeString(tokenHex)
	if err != nil || len(token) != 4 {
		return nil
	}

	buf := &bytes.Buffer{}
	cmdByte := byte(0x00)
	withSlot := false
	var slotByte byte

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
		withSlot = true
	case "eject":
		cmdByte = 0x80
		withSlot = true
	default:
		return nil
	}

	if withSlot {
		slotInt, err := strconv.Atoi(slotStr)
		if err != nil || slotInt < 1 || slotInt > 255 {
			return nil
		}
		slotByte = byte(slotInt)
	}

	packLen := uint16(7)
	if withSlot {
		packLen++
	}
	binary.Write(buf, binary.BigEndian, packLen)
	buf.WriteByte(cmdByte)
	buf.WriteByte(0x01)
	buf.WriteByte(0x00)
	buf.Write(token)
	if withSlot {
		buf.WriteByte(slotByte)
	}

	data := buf.Bytes()
	data[4] = xorChecksum(data[5:])

	return data
}

func HandleIncoming(data []byte) []byte {
	if len(data) < 7 {
		return nil
	}
	cmd := data[2]
	token := data[5:9]
	switch cmd {
	case 0x60:
		log.Println("Received login")
		resp := append([]byte{0x00, 0x08, 0x60, 0x01, 0x01}, token...)
		resp = append(resp, 0x01)
		return resp
	case 0x61:
		log.Println("Received heartbeat")
		return data[:7]
	case 0x66:
		log.Printf("Received Return Power Bank from slot %d, ID: %x", data[9], data[10:18])
	default:
		log.Printf("Received unhandled cmd: 0x%02x", cmd)
	}
	return nil
}
