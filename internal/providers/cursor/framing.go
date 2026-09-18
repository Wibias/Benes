package cursor

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	connectHeaderBytes = 5
	connectFlagEnd     = 0x02
	maxConnectPayload  = 16 << 20
)

var (
	ErrFrameIncomplete = errors.New("Cursor Connect frame is incomplete")
	ErrFrameTooLarge   = errors.New("Cursor Connect frame exceeds raw-byte cap")
)

type ConnectFrame struct {
	EndStream bool
	Payload   []byte
}

func EncodeConnectFrame(payload []byte, endStream bool) ([]byte, error) {
	if len(payload) > maxConnectPayload {
		return nil, ErrFrameTooLarge
	}
	out := make([]byte, connectHeaderBytes+len(payload))
	if endStream {
		out[0] = connectFlagEnd
	}
	binary.BigEndian.PutUint32(out[1:5], uint32(len(payload)))
	copy(out[5:], payload)
	return out, nil
}

func DecodeConnectFrame(raw []byte) (ConnectFrame, int, error) {
	if len(raw) < connectHeaderBytes {
		return ConnectFrame{}, 0, ErrFrameIncomplete
	}
	length := binary.BigEndian.Uint32(raw[1:5])
	if int(length) > maxConnectPayload {
		return ConnectFrame{}, 0, ErrFrameTooLarge
	}
	need := connectHeaderBytes + int(length)
	if len(raw) < need {
		return ConnectFrame{}, 0, ErrFrameIncomplete
	}
	payload := make([]byte, length)
	copy(payload, raw[connectHeaderBytes:need])
	return ConnectFrame{EndStream: raw[0]&connectFlagEnd != 0, Payload: payload}, need, nil
}

func DecodeConnectFrames(raw []byte) (frames []ConnectFrame, remainder []byte, err error) {
	offset := 0
	for offset < len(raw) {
		frame, n, decErr := DecodeConnectFrame(raw[offset:])
		if decErr != nil {
			if errors.Is(decErr, ErrFrameIncomplete) {
				return frames, raw[offset:], nil
			}
			return nil, nil, fmt.Errorf("decode Connect frame: %w", decErr)
		}
		frames = append(frames, frame)
		offset += n
	}
	return frames, nil, nil
}
