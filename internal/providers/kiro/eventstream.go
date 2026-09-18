package kiro

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

const (
	preludeLen        = 8
	preludeCRCLen     = 4
	messageCRCLen     = 4
	headerBlockOffset = preludeLen + preludeCRCLen
	minMessageLen     = headerBlockOffset + messageCRCLen
	maxMessageLen     = 16 << 20
	maxHeadersLen     = 128 << 10
)

type EventStreamMessage struct {
	Headers map[string]string
	Payload []byte
}

func DecodeEventStreamMessage(frame []byte) (EventStreamMessage, error) {
	if len(frame) < minMessageLen {
		return EventStreamMessage{}, fmt.Errorf("Kiro event-stream frame is truncated")
	}
	total := int(binary.BigEndian.Uint32(frame[:4]))
	if total != len(frame) {
		return EventStreamMessage{}, fmt.Errorf("Kiro event-stream framed length mismatch")
	}
	if total > maxMessageLen {
		return EventStreamMessage{}, fmt.Errorf("Kiro event-stream frame exceeds byte cap")
	}
	headersLen := int(binary.BigEndian.Uint32(frame[4:8]))
	preludeCRC := binary.BigEndian.Uint32(frame[8:12])
	if crc32.ChecksumIEEE(frame[:preludeLen]) != preludeCRC {
		return EventStreamMessage{}, fmt.Errorf("Kiro event-stream prelude CRC mismatch")
	}
	if headersLen > maxHeadersLen || headersLen > total-minMessageLen {
		return EventStreamMessage{}, fmt.Errorf("Kiro event-stream headers exceed byte cap")
	}
	msgCRC := binary.BigEndian.Uint32(frame[total-messageCRCLen:])
	if crc32.ChecksumIEEE(frame[:total-messageCRCLen]) != msgCRC {
		return EventStreamMessage{}, fmt.Errorf("Kiro event-stream message CRC mismatch")
	}
	headers, err := parseEventStreamHeaders(frame[headerBlockOffset : headerBlockOffset+headersLen])
	if err != nil {
		return EventStreamMessage{}, err
	}
	payload := append([]byte(nil), frame[headerBlockOffset+headersLen:total-messageCRCLen]...)
	return EventStreamMessage{Headers: headers, Payload: payload}, nil
}

func parseEventStreamHeaders(raw []byte) (map[string]string, error) {
	out := map[string]string{}
	i := 0
	for i < len(raw) {
		if i >= len(raw) {
			break
		}
		nameLen := int(raw[i])
		i++
		if i+nameLen+1 > len(raw) {
			return nil, fmt.Errorf("Kiro event-stream header is truncated")
		}
		name := string(raw[i : i+nameLen])
		i += nameLen
		valueType := raw[i]
		i++
		switch valueType {
		case 7: // string
			if i+2 > len(raw) {
				return nil, fmt.Errorf("Kiro event-stream header is truncated")
			}
			ln := int(binary.BigEndian.Uint16(raw[i : i+2]))
			i += 2
			if i+ln > len(raw) {
				return nil, fmt.Errorf("Kiro event-stream header is truncated")
			}
			out[name] = string(raw[i : i+ln])
			i += ln
		default:
			return nil, fmt.Errorf("Kiro event-stream header type %d is unsupported", valueType)
		}
	}
	return out, nil
}

func EncodeEventStreamMessage(headers map[string]string, payload []byte) []byte {
	var headerBuf []byte
	for name, value := range headers {
		headerBuf = append(headerBuf, byte(len(name)))
		headerBuf = append(headerBuf, name...)
		headerBuf = append(headerBuf, 7)
		var ln [2]byte
		binary.BigEndian.PutUint16(ln[:], uint16(len(value)))
		headerBuf = append(headerBuf, ln[:]...)
		headerBuf = append(headerBuf, value...)
	}
	total := headerBlockOffset + len(headerBuf) + len(payload) + messageCRCLen
	out := make([]byte, total)
	binary.BigEndian.PutUint32(out[0:4], uint32(total))
	binary.BigEndian.PutUint32(out[4:8], uint32(len(headerBuf)))
	binary.BigEndian.PutUint32(out[8:12], crc32.ChecksumIEEE(out[:preludeLen]))
	copy(out[headerBlockOffset:], headerBuf)
	copy(out[headerBlockOffset+len(headerBuf):], payload)
	binary.BigEndian.PutUint32(out[total-messageCRCLen:], crc32.ChecksumIEEE(out[:total-messageCRCLen]))
	return out
}
