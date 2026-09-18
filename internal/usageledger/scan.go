package usageledger

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
)

const defaultMaxLine = 4 << 20

func scanSection(file *os.File, size, start int64, maxLine int) (lines [][]byte, extraDiscard int64, err error) {
	if file == nil || size <= 0 || start >= size {
		return nil, 0, nil
	}
	if start < 0 {
		start = 0
	}
	if maxLine <= 0 {
		maxLine = defaultMaxLine
	}
	section := io.NewSectionReader(file, start, size-start)
	reader := bufio.NewReaderSize(section, 64*1024)
	if start > 0 {
		var prev [1]byte
		n, readErr := file.ReadAt(prev[:], start-1)
		aligned := readErr == nil && n == 1 && prev[0] == '\n'
		if !aligned {
			discarded, discardErr := discardPartialLine(reader)
			extraDiscard = discarded
			if discardErr != nil && discardErr != io.EOF {
				return nil, extraDiscard, discardErr
			}
			if discardErr == io.EOF {
				return nil, extraDiscard, nil
			}
		}
	}
	lines, err = scanCommittedLines(reader, maxLine)
	return lines, extraDiscard, err
}

func discardPartialLine(reader *bufio.Reader) (int64, error) {
	var n int64
	for {
		fragment, err := reader.ReadSlice('\n')
		n += int64(len(fragment))
		if err == bufio.ErrBufferFull {
			continue
		}
		return n, err
	}
}

func scanCommittedLines(reader *bufio.Reader, maxLine int) ([][]byte, error) {
	var (
		lines     [][]byte
		buf       []byte
		oversized bool
	)
	for {
		fragment, err := reader.ReadSlice('\n')
		hasNewline := len(fragment) > 0 && fragment[len(fragment)-1] == '\n'
		payload := fragment
		if hasNewline {
			payload = fragment[:len(fragment)-1]
			if len(payload) > 0 && payload[len(payload)-1] == '\r' {
				payload = payload[:len(payload)-1]
			}
		}
		if len(payload) > 0 && !oversized {
			if maxLine > 0 && len(buf)+len(payload) > maxLine {
				oversized = true
				buf = buf[:0]
			} else {
				buf = append(buf, payload...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			return lines, nil
		}
		if err != nil {
			return lines, err
		}
		if !hasNewline {
			return lines, nil
		}
		line := bytesClone(bytes.TrimSpace(buf))
		buf = buf[:0]
		skip := oversized
		oversized = false
		if skip || len(line) == 0 {
			continue
		}
		lines = append(lines, line)
	}
}

func bytesClone(buf []byte) []byte {
	if len(buf) == 0 {
		return nil
	}
	out := make([]byte, len(buf))
	copy(out, buf)
	return out
}

func recordBoundary(file *os.File, localStart int64) error {
	if localStart <= 0 {
		return nil
	}
	var prev [1]byte
	n, err := file.ReadAt(prev[:], localStart-1)
	if err != nil || n != 1 || prev[0] != '\n' {
		return fmt.Errorf("%w", ErrUnalignedOffset)
	}
	return nil
}

func scanRecords(reader *bufio.Reader, maxLine int, fn func(line []byte, rawBytes int64, oversized bool) error) error {
	if fn == nil {
		return fmt.Errorf("enumerator is nil")
	}
	if maxLine <= 0 {
		maxLine = defaultMaxLine
	}
	var (
		buf       []byte
		raw       int64
		oversized bool
	)
	for {
		fragment, err := reader.ReadSlice('\n')
		raw += int64(len(fragment))
		hasNewline := len(fragment) > 0 && fragment[len(fragment)-1] == '\n'
		payload := fragment
		if hasNewline {
			payload = fragment[:len(fragment)-1]
			if len(payload) > 0 && payload[len(payload)-1] == '\r' {
				payload = payload[:len(payload)-1]
			}
		}
		if len(payload) > 0 && !oversized {
			if maxLine > 0 && len(buf)+len(payload) > maxLine {
				oversized = true
				buf = buf[:0]
			} else {
				buf = append(buf, payload...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF || !hasNewline {
			return nil
		}
		if err != nil {
			return err
		}
		line := bytesClone(bytes.TrimSpace(buf))
		skip := oversized
		buf = buf[:0]
		oversized = false
		yieldErr := fn(line, raw, skip)
		raw = 0
		if yieldErr != nil {
			return yieldErr
		}
	}
}
