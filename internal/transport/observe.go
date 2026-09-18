package transport

import (
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/Wibias/Benes/internal/timeline"
)

func Observe(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return observingRoundTripper{next: next}
}

type observingRoundTripper struct {
	next http.RoundTripper
}

func (r observingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	tr := timeline.FromContext(request.Context())
	response, err := r.next.RoundTrip(request)
	if tr == nil {
		return response, err
	}
	if err != nil {
		tr.Mark(timeline.StageUpstreamWaitHeaders, timeline.SideUpstream, timeline.MilestoneHeaders, false, "headers")
		return nil, err
	}
	tr.Mark(timeline.StageUpstreamWaitHeaders, timeline.SideUpstream, timeline.MilestoneHeaders, true, "")
	if response != nil && response.Body != nil {
		response.Body = &firstByteBody{ReadCloser: response.Body, tr: tr}
	}
	return response, nil
}

type firstByteBody struct {
	io.ReadCloser
	tr   *timeline.Trace
	once sync.Once
}

func (b *firstByteBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.once.Do(func() {
			b.tr.Mark(timeline.StageUpstreamRead, timeline.SideUpstream, timeline.MilestoneFirstByte, true, "")
		})
	}
	if errors.Is(err, io.EOF) {
		b.tr.Mark(timeline.StageUpstreamRead, timeline.SideUpstream, timeline.MilestoneUpstreamEnd, true, "")
	}
	return n, err
}
