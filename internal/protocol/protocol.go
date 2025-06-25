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
	// Checksum вычисляется для payload (данные после Token)
	if len(data) > 9 {
		calculated := xorChecksum(data[9:])
		log.Printf("Checksum validation: expected=0x%02x, calculated=0x%02x", expected, calculated)
		return expected == calculated
	}
	// Для пакетов без payload checksum должен быть 0x00
	log.Printf("No payload, checksum should be 0x00, got 0x%02x", expected)
	return expected == 0x00
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
	var packLen uint16 = 7 // Базовая длина: PackLen(2) + Cmd(1) + Version(1) + CheckSum(1) + Token(4)

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
	case "query_power_bank":
		cmdByte = 0x64
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
	case "voice_set":
		cmdByte = 0x70
		if level, err := strconv.Atoi(slotStr); err == nil && level >= 0 && level <= 15 {
			payload = []byte{byte(level)}
			packLen++
		} else {
			log.Printf("Invalid voice level: %s", slotStr)
			return nil
		}
	case "set_server":
		cmdByte = 0x63
		// Для простоты используем slotStr как heartbeat interval
		if interval, err := strconv.Atoi(slotStr); err == nil && interval > 0 {
			// Пример: устанавливаем тот же сервер с новым интервалом
			address := "127.0.0.1"
			port := "9000"

			addressBytes := []byte(address)
			addressBytes = append(addressBytes, 0x00) // null terminator
			portBytes := []byte(port)
			portBytes = append(portBytes, 0x00) // null terminator

			payload = append(payload, byte(len(addressBytes)>>8), byte(len(addressBytes))) // AddressLen
			payload = append(payload, addressBytes...)
			payload = append(payload, byte(len(portBytes)>>8), byte(len(portBytes))) // PortLen
			payload = append(payload, portBytes...)
			payload = append(payload, byte(interval)) // Heartbeat interval

			packLen += uint16(len(payload))
		} else {
			log.Printf("Invalid heartbeat interval: %s", slotStr)
			return nil
		}
	default:
		log.Printf("Unknown command: %s", cmd)
		return nil
	}

	// Записываем пакет
	binary.Write(buf, binary.BigEndian, packLen)
	buf.WriteByte(cmdByte)
	buf.WriteByte(0x01) // Version

	// Вычисляем checksum для payload
	var checksum byte = 0x00
	if len(payload) > 0 {
		checksum = xorChecksum(payload)
	}
	buf.WriteByte(checksum)

	buf.Write(token)
	if len(payload) > 0 {
		buf.Write(payload)
	}

	return buf.Bytes()
}

func HandleIncoming(data []byte) ([]byte, string) {
	if len(data) < 7 {
		log.Printf("Packet too short: %d bytes", len(data))
		return nil, ""
	}

	packLen := binary.BigEndian.Uint16(data[0:2])
	log.Printf("PackLen from header: %d, actual data length: %d", packLen, len(data))

	// PackLen включает в себя весь пакет, включая поле PackLen
	if int(packLen) != len(data) {
		log.Printf("Packet length mismatch: expected %d, got %d", packLen, len(data))
		// Не возвращаем ошибку, продолжаем обработку
	}

	if !validateChecksum(data) {
		log.Printf("Invalid checksum")
		return nil, ""
	}

	cmd := data[2]
	version := data[3]
	checksum := data[4]
	token := data[5:9]
	var stationID string

	log.Printf("Handling command 0x%02x, version %d, checksum 0x%02x", cmd, version, checksum)

	switch cmd {
	case 0x60: // Login
		log.Println("Received login")
		log.Printf("Full packet: %x", data)

		if len(data) >= 21 { // Минимальная длина для Login
			rand := data[9:13]
			magic := binary.BigEndian.Uint16(data[13:15])
			boxIDLen := binary.BigEndian.Uint16(data[15:17])

			log.Printf("Login: rand=%x, magic=0x%04x, boxIDLen=%d", rand, magic, boxIDLen)

			if len(data) >= int(17+boxIDLen) {
				boxIDBytes := data[17 : 17+boxIDLen]
				// Убираем null terminator если он есть
				if len(boxIDBytes) > 0 && boxIDBytes[len(boxIDBytes)-1] == 0x00 {
					stationID = string(boxIDBytes[:len(boxIDBytes)-1])
				} else {
					stationID = string(boxIDBytes)
				}
				log.Printf("Station ID: %s", stationID)

				// Проверяем есть ли еще данные (ReqDataLen + ReqData)
				remainingPos := 17 + int(boxIDLen)
				if len(data) > remainingPos+2 {
					reqDataLen := binary.BigEndian.Uint16(data[remainingPos : remainingPos+2])
					log.Printf("ReqDataLen: %d", reqDataLen)
					if len(data) >= remainingPos+2+int(reqDataLen) {
						reqData := data[remainingPos+2 : remainingPos+2+int(reqDataLen)]
						log.Printf("ReqData: %x", reqData)
					}
				}
			}
		}

		// Создаем ответ на Login
		resp := bytes.NewBuffer(nil)
		binary.Write(resp, binary.BigEndian, uint16(8))
		resp.WriteByte(0x60) // Cmd
		resp.WriteByte(0x01) // Version
		resp.WriteByte(0x01) // CheckSum (XOR result of payload)
		resp.Write(token)    // Token
		resp.WriteByte(0x01) // Success

		respBytes := resp.Bytes()
		// Пересчитываем checksum для payload (только Result byte)
		respBytes[4] = xorChecksum(respBytes[9:])
		return respBytes, stationID

	case 0x61: // Heartbeat
		log.Println("Received heartbeat")
		// Просто возвращаем тот же пакет
		return data, ""

	case 0x66: // Return Power Bank
		if len(data) >= 18 {
			slot := data[9]
			powerBankID := data[10:18]
			log.Printf("Received Return Power Bank from slot %d, ID: %x", slot, powerBankID)

			resp := bytes.NewBuffer(nil)
			binary.Write(resp, binary.BigEndian, uint16(9))
			resp.WriteByte(0x66) // Cmd
			resp.WriteByte(0x01) // Version
			resp.WriteByte(0x00) // CheckSum placeholder
			resp.Write(token)    // Token
			resp.WriteByte(slot) // Slot
			resp.WriteByte(0x01) // Success

			respBytes := resp.Bytes()
			respBytes[4] = xorChecksum(respBytes[9:])
			return respBytes, ""
		}

	case 0x62: // Query Firmware Version
		log.Println("Received query firmware version")
		fwVersion := "RL1,H6,08,14"
		fwVersionBytes := []byte(fwVersion)
		fwVersionBytes = append(fwVersionBytes, 0x00) // null terminator
		fwLen := uint16(len(fwVersionBytes))

		resp := bytes.NewBuffer(nil)
		binary.Write(resp, binary.BigEndian, uint16(7+2+fwLen))
		resp.WriteByte(0x62) // Cmd
		resp.WriteByte(0x01) // Version
		resp.WriteByte(0x00) // CheckSum placeholder
		resp.Write(token)    // Token
		binary.Write(resp, binary.BigEndian, fwLen)
		resp.Write(fwVersionBytes)

		respBytes := resp.Bytes()
		respBytes[4] = xorChecksum(respBytes[9:])
		return respBytes, ""

	case 0x65: // Rent Power Bank
		if len(data) >= 10 {
			slot := data[9]
			log.Printf("Received rent power bank request for slot %d", slot)

			powerBankID := []byte("RL1A|00d") // 8 bytes exactly
			if len(powerBankID) < 8 {
				// Дополняем до 8 байт
				for len(powerBankID) < 8 {
					powerBankID = append(powerBankID, 0x00)
				}
			} else if len(powerBankID) > 8 {
				powerBankID = powerBankID[:8]
			}

			resp := bytes.NewBuffer(nil)
			binary.Write(resp, binary.BigEndian, uint16(17))
			resp.WriteByte(0x65)    // Cmd
			resp.WriteByte(0x01)    // Version
			resp.WriteByte(0x00)    // CheckSum placeholder
			resp.Write(token)       // Token
			resp.WriteByte(slot)    // Slot
			resp.WriteByte(0x01)    // Success
			resp.Write(powerBankID) // PowerBankID (8 bytes)

			respBytes := resp.Bytes()
			respBytes[4] = xorChecksum(respBytes[9:])
			return respBytes, ""
		}

	case 0x80: // Eject Power Bank
		if len(data) >= 10 {
			slot := data[9]
			log.Printf("Received eject power bank request for slot %d", slot)

			powerBankID := []byte("RL1A|00d") // 8 bytes exactly
			if len(powerBankID) < 8 {
				for len(powerBankID) < 8 {
					powerBankID = append(powerBankID, 0x00)
				}
			} else if len(powerBankID) > 8 {
				powerBankID = powerBankID[:8]
			}

			resp := bytes.NewBuffer(nil)
			binary.Write(resp, binary.BigEndian, uint16(17))
			resp.WriteByte(0x80)    // Cmd
			resp.WriteByte(0x01)    // Version
			resp.WriteByte(0x00)    // CheckSum placeholder
			resp.Write(token)       // Token
			resp.WriteByte(slot)    // Slot
			resp.WriteByte(0x01)    // Success
			resp.Write(powerBankID) // PowerBankID (8 bytes)

			respBytes := resp.Bytes()
			respBytes[4] = xorChecksum(respBytes[9:])
			return respBytes, ""
		}

	case 0x69: // Query ICCID
		log.Println("Received query ICCID")
		iccid := "89860416121880245965"
		iccidBytes := []byte(iccid)
		iccidBytes = append(iccidBytes, 0x00) // null terminator
		iccidLen := uint16(len(iccidBytes))

		resp := bytes.NewBuffer(nil)
		binary.Write(resp, binary.BigEndian, uint16(7+2+iccidLen))
		resp.WriteByte(0x69) // Cmd
		resp.WriteByte(0x01) // Version
		resp.WriteByte(0x00) // CheckSum placeholder
		resp.Write(token)    // Token
		binary.Write(resp, binary.BigEndian, iccidLen)
		resp.Write(iccidBytes)

		respBytes := resp.Bytes()
		respBytes[4] = xorChecksum(respBytes[9:])
		return respBytes, ""

	case 0x77: // Get Voice Level
		log.Println("Received get voice level")
		resp := bytes.NewBuffer(nil)
		binary.Write(resp, binary.BigEndian, uint16(8))
		resp.WriteByte(0x77) // Cmd
		resp.WriteByte(0x01) // Version
		resp.WriteByte(0x00) // CheckSum placeholder
		resp.Write(token)    // Token
		resp.WriteByte(0x0e) // Voice level (14)

		respBytes := resp.Bytes()
		respBytes[4] = xorChecksum(respBytes[9:])
		return respBytes, ""

	case 0x70: // Set Voice Level
		if len(data) >= 10 {
			level := data[9]
			log.Printf("Received set voice level to %d", level)

			resp := bytes.NewBuffer(nil)
			binary.Write(resp, binary.BigEndian, uint16(7))
			resp.WriteByte(0x70) // Cmd
			resp.WriteByte(0x01) // Version
			resp.WriteByte(0x00) // CheckSum (no payload)
			resp.Write(token)    // Token

			return resp.Bytes(), ""
		}

	case 0x64: // Query Power Bank Information
		log.Println("Received query power bank information")

		// Пример: 2 power bank в слотах 1 и 3
		resp := bytes.NewBuffer(nil)

		// Сначала считаем общую длину
		remainNum := byte(2)
		slot1Data := []byte{0x01}          // slot 1
		powerBank1ID := []byte("RL1H|001") // 8 bytes
		if len(powerBank1ID) < 8 {
			for len(powerBank1ID) < 8 {
				powerBank1ID = append(powerBank1ID, 0x00)
			}
		} else if len(powerBank1ID) > 8 {
			powerBank1ID = powerBank1ID[:8]
		}
		level1 := byte(4) // 81-100%

		slot3Data := []byte{0x03}          // slot 3
		powerBank3ID := []byte("RL1H|003") // 8 bytes
		if len(powerBank3ID) < 8 {
			for len(powerBank3ID) < 8 {
				powerBank3ID = append(powerBank3ID, 0x00)
			}
		} else if len(powerBank3ID) > 8 {
			powerBank3ID = powerBank3ID[:8]
		}
		level3 := byte(2) // 41-60%

		payloadLen := 1 + (1+8+1)*2 // RemainNum + (Slot+PowerBankID+Level)*2
		totalLen := uint16(7 + payloadLen)

		binary.Write(resp, binary.BigEndian, totalLen)
		resp.WriteByte(0x64) // Cmd
		resp.WriteByte(0x01) // Version
		resp.WriteByte(0x00) // CheckSum placeholder
		resp.Write(token)    // Token

		// Payload
		resp.WriteByte(remainNum)
		resp.Write(slot1Data)
		resp.Write(powerBank1ID)
		resp.WriteByte(level1)
		resp.Write(slot3Data)
		resp.Write(powerBank3ID)
		resp.WriteByte(level3)

		respBytes := resp.Bytes()
		respBytes[4] = xorChecksum(respBytes[9:])
		return respBytes, ""

	case 0x67: // Restart
		log.Println("Received restart command")
		// Просто возвращаем подтверждение
		resp := bytes.NewBuffer(nil)
		binary.Write(resp, binary.BigEndian, uint16(7))
		resp.WriteByte(0x67) // Cmd
		resp.WriteByte(0x01) // Version
		resp.WriteByte(0x00) // CheckSum (no payload)
		resp.Write(token)    // Token

		return resp.Bytes(), ""

	case 0x63: // Set server address
		if len(data) >= 10 {
			log.Println("Received set server address")
			// Просто возвращаем подтверждение
			resp := bytes.NewBuffer(nil)
			binary.Write(resp, binary.BigEndian, uint16(7))
			resp.WriteByte(0x63) // Cmd
			resp.WriteByte(0x01) // Version
			resp.WriteByte(0x00) // CheckSum (no payload)
			resp.Write(token)    // Token

			return resp.Bytes(), ""
		}

	default:
		log.Printf("Received unhandled command: 0x%02x", cmd)
	}

	return nil, ""
}
