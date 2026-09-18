package sse

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrLineTooLarge  = errors.New("server-sent event line exceeds configured byte limit")
	ErrEventTooLarge = errors.New("server-sent event data exceeds configured byte limit")
)

type Limits struct {
	MaxLineBytes  int
	MaxEventBytes int
}

type Event struct {
	Type              string
	Data              string
	ID                string
	RetryMilliseconds int64
}

type Decoder struct {
	reader      *bufio.Reader
	limits      Limits
	firstLine   bool
	lastEventID string
	retryMillis int64
}

func NewDecoder(reader io.Reader, limits Limits) *Decoder {
	if limits.MaxLineBytes <= 0 {
		limits.MaxLineBytes = 1 << 20
	}
	if limits.MaxEventBytes <= 0 {
		limits.MaxEventBytes = 4 << 20
	}
	return &Decoder{
		reader:    bufio.NewReaderSize(reader, 32*1024),
		limits:    limits,
		firstLine: true,
	}
}

func (d *Decoder) Next() (Event, error) {
	var data strings.Builder
	eventType := ""

	for {
		line, eof, err := d.readLine()
		if err != nil {
			return Event{}, err
		}
		if eof {
			// WHATWG requires pending data to be discarded when EOF arrives before
			// the blank line that dispatches an event.
			return Event{}, io.EOF
		}
		if d.firstLine {
			d.firstLine = false
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" {
			if data.Len() == 0 {
				eventType = ""
				continue
			}
			payload := data.String()
			payload = strings.TrimSuffix(payload, "\n")
			if eventType == "" {
				eventType = "message"
			}
			return Event{
				Type:              eventType,
				Data:              payload,
				ID:                d.lastEventID,
				RetryMilliseconds: d.retryMillis,
			}, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		field := line
		value := ""
		if index := strings.IndexByte(line, ':'); index >= 0 {
			field = line[:index]
			value = line[index+1:]
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
		}

		switch field {
		case "event":
			eventType = value
		case "data":
			if data.Len()+len(value)+1 > d.limits.MaxEventBytes {
				return Event{}, ErrEventTooLarge
			}
			data.WriteString(value)
			data.WriteByte('\n')
		case "id":
			if !strings.ContainsRune(value, '\x00') {
				d.lastEventID = value
			}
		case "retry":
			if isASCIIDigits(value) {
				parsed, err := strconv.ParseInt(value, 10, 64)
				if err == nil {
					d.retryMillis = parsed
				}
			}
		}
	}
}

func (d *Decoder) readLine() (line string, eof bool, err error) {
	var buffer bytes.Buffer
	for {
		value, readErr := d.reader.ReadByte()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if buffer.Len() == 0 {
					return "", true, nil
				}
				return makeValidUTF8(buffer.Bytes()), true, nil
			}
			return "", false, readErr
		}
		if value == '\n' {
			return makeValidUTF8(buffer.Bytes()), false, nil
		}
		if value == '\r' {
			if next, peekErr := d.reader.Peek(1); peekErr == nil && next[0] == '\n' {
				_, _ = d.reader.ReadByte()
			}
			return makeValidUTF8(buffer.Bytes()), false, nil
		}
		if buffer.Len()+1 > d.limits.MaxLineBytes {
			return "", false, ErrLineTooLarge
		}
		buffer.WriteByte(value)
	}
}

func makeValidUTF8(value []byte) string {
	if utf8.Valid(value) {
		return string(value)
	}
	return strings.ToValidUTF8(string(value), "\ufffd")
}

func isASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func WriteJSON(writer io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal SSE JSON: %w", err)
	}
	if bytes.IndexAny(payload, "\r\n") >= 0 {
		return fmt.Errorf("marshal SSE JSON produced literal line break")
	}
	if _, err := writer.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	_, err = writer.Write([]byte("\n\n"))
	return err
}
