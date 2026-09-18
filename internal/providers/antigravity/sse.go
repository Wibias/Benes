package antigravity

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
)

var (
	ErrFrameTooLarge     = errors.New("Cloud Code Assist SSE frame exceeds raw-byte cap")
	ErrFailoverForbidden = errors.New("Cloud Code Assist peer failover is no longer allowed")
)

type FailoverClass string

const (
	Failover404         FailoverClass = "404"
	Failover503         FailoverClass = "503"
	FailoverEmpty       FailoverClass = "empty"
	FailoverUnavailable FailoverClass = "unavailable"
)

type Decoder struct {
	scanner    *bufio.Scanner
	maxFrame   int
	sawContent bool
	failedOver bool
	totalRaw   int
}

func NewDecoder(r io.Reader, maxFrame int) *Decoder {
	if maxFrame <= 0 {
		maxFrame = 256 << 10
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxFrame+1)
	scanner.Split(scanRawLines)
	return &Decoder{scanner: scanner, maxFrame: maxFrame}
}

func (d *Decoder) Next() (string, error) {
	if d == nil || d.scanner == nil {
		return "", io.EOF
	}
	if !d.scanner.Scan() {
		if err := d.scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	raw := d.scanner.Bytes()
	d.totalRaw += len(raw)
	if len(raw) > d.maxFrame {
		return "", ErrFrameTooLarge
	}
	line := string(raw)
	if strings.HasPrefix(line, "data:") {
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload != "" && payload != "{}" && !strings.Contains(payload, `"error"`) {
			d.sawContent = true
		}
	}
	return line, nil
}

func (d *Decoder) AllowPeerFailover(class FailoverClass) error {
	if d == nil {
		return ErrFailoverForbidden
	}
	if d.sawContent || d.failedOver {
		return ErrFailoverForbidden
	}
	switch class {
	case Failover404, Failover503, FailoverEmpty, FailoverUnavailable:
		d.failedOver = true
		return nil
	default:
		return ErrFailoverForbidden
	}
}

func (d *Decoder) SawContent() bool {
	return d != nil && d.sawContent
}

func scanRawLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
