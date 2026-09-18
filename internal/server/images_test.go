package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/timeline"
)

type fakeImageProvider struct {
	fakeProvider
	called   bool
	opened   bool
	last     providercontract.ImageRelayRequest
	dispatch providercontract.DispatchRequest
	status   int
	header   http.Header
	payload  []byte
	err      error
	block    <-chan struct{}
}

func (p *fakeImageProvider) Open(ctx context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	p.opened = true
	return p.fakeProvider.Open(ctx, dispatch)
}

func (p *fakeImageProvider) SupportsImageRelay() bool { return true }

func (p *fakeImageProvider) RelayImage(ctx context.Context, dispatch providercontract.DispatchRequest, req providercontract.ImageRelayRequest) (int, http.Header, []byte, error) {
	p.called = true
	p.dispatch = dispatch
	p.last = req
	if p.block != nil {
		select {
		case <-ctx.Done():
			return 0, nil, nil, ctx.Err()
		case <-p.block:
		}
	}
	if p.err != nil {
		return 0, nil, nil, p.err
	}
	status := p.status
	if status == 0 {
		status = http.StatusOK
	}
	payload := p.payload
	if payload == nil {
		payload = []byte(`{"created":1,"data":[{"b64_json":"QQ=="}]}`)
	}
	return status, p.header, append([]byte(nil), payload...), nil
}

func postImages(t *testing.T, h http.Handler, path, body, contentType, auth string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestImagesGenerationsRelaysEligibleUpstream(t *testing.T) {
	upstream := &fakeImageProvider{}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": upstream},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	body := `{"model":"openai-apikey/gpt-image-2","prompt":"a cat","n":1}`
	rr := postImages(t, h, imageGenerationsPath, body, "application/json", "Bearer local-secret")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !upstream.called {
		t.Fatal("eligible image upstream was not called")
	}
	if upstream.opened {
		t.Fatal("chat Open() must not serve image generation")
	}
	if upstream.last.Kind != providercontract.ImageRelayGenerations {
		t.Fatalf("kind=%q", upstream.last.Kind)
	}
	if !bytes.Contains(upstream.last.Body, []byte(`"prompt":"a cat"`)) {
		t.Fatalf("body=%s", upstream.last.Body)
	}
	if got := upstream.dispatch.ForwardHeaders.Get("authorization"); got != "" {
		t.Fatalf("admission bearer leaked to image relay: %q", got)
	}
	if !strings.Contains(rr.Body.String(), `"b64_json"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestImagesEditsRelaysBoundedMultipart(t *testing.T) {
	upstream := &fakeImageProvider{}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": upstream},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", "openai-apikey/gpt-image-2"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("prompt", "make it a sketch"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("image[]", "cat.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("fake-png-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, imageEditsPath, bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if upstream.last.Kind != providercontract.ImageRelayEdits {
		t.Fatalf("kind=%q", upstream.last.Kind)
	}
	if !strings.HasPrefix(upstream.last.ContentType, "multipart/form-data") {
		t.Fatalf("content-type=%q", upstream.last.ContentType)
	}
	if !bytes.Contains(upstream.last.Body, []byte("fake-png-bytes")) {
		t.Fatalf("multipart body lost image bytes")
	}
}

func TestImagesNoEligibleUpstreamIsActionable400(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"kiro": &fakeProvider{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := postImages(t, h, imageGenerationsPath, `{"model":"gpt-image-2","prompt":"a cat"}`, "application/json", "Bearer local-secret")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(strings.ToLower(rr.Body.String()), "internal") {
		t.Fatalf("opaque error: %s", rr.Body.String())
	}
	if !strings.Contains(strings.ToLower(rr.Body.String()), "image") {
		t.Fatalf("missing actionable image diagnostic: %s", rr.Body.String())
	}
}

func TestImagesDoesNotUseTextOnlyProvider(t *testing.T) {
	textOnly := &fakeProvider{}
	images := &fakeImageProvider{}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{
			"kiro":          textOnly,
			"openai-apikey": images,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := postImages(t, h, imageGenerationsPath, `{"model":"kiro/gpt-image-2","prompt":"a cat"}`, "application/json", "Bearer local-secret")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if images.called {
		t.Fatal("must not silently reroute a text-only selector onto another provider")
	}
}

func TestImagesUnknownV1PathStill404(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeImageProvider{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := postImages(t, h, "/v1/images/variations", `{"model":"openai-apikey/gpt-image-2"}`, "application/json", "Bearer local-secret")
	if rr.Code != http.StatusNotFound || !strings.Contains(rr.Body.String(), `"code":"not_found"`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	get := httptest.NewRequest(http.MethodGet, imageGenerationsPath, nil)
	get.Header.Set("Authorization", "Bearer local-secret")
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	if getRR.Code != http.StatusNotFound {
		t.Fatalf("GET status=%d body=%s", getRR.Code, getRR.Body.String())
	}
}

func TestImagesPreflightIsDataPlaneCORS(t *testing.T) {
	h, err := NewHandler(Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{
			BindHostname:      "0.0.0.0",
			DataPlaneTokens:   []string{"secret"},
			CORSAllowOrigins:  []string{"https://dashboard.example"},
			CORSDefaultOrigin: "http://localhost:23100",
		},
		Providers: map[string]Provider{"openai-apikey": &fakeImageProvider{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodOptions, imageGenerationsPath, nil)
	req.Host = "gateway.example:23100"
	req.Header.Set("Origin", "https://dashboard.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestImagesRequiresAdmission(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeImageProvider{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := postImages(t, h, imageGenerationsPath, `{"model":"openai-apikey/gpt-image-2","prompt":"a cat"}`, "application/json", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestImagesPreservesUpstreamStatus(t *testing.T) {
	upstream := &fakeImageProvider{
		status:  http.StatusPaymentRequired,
		payload: []byte(`{"error":{"message":"plan does not include image generation","type":"invalid_request_error"}}`),
		header:  http.Header{"Content-Type": []string{"application/json"}},
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": upstream},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := postImages(t, h, imageGenerationsPath, `{"model":"openai-apikey/gpt-image-2","prompt":"a cat"}`, "application/json", "Bearer local-secret")
	if rr.Code != http.StatusPaymentRequired {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "plan does not include image generation") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestImagesRejectsOversizedBody(t *testing.T) {
	upstream := &fakeImageProvider{}
	h, err := NewHandler(Options{
		DataPlaneToken:       "local-secret",
		Providers:            map[string]Provider{"openai-apikey": upstream},
		MaxRequestBytes:      32,
		ImageMaxRequestBytes: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := postImages(t, h, imageGenerationsPath, `{"model":"openai-apikey/gpt-image-2","prompt":"`+strings.Repeat("x", 80)+`"}`, "application/json", "Bearer local-secret")
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if upstream.called {
		t.Fatal("oversized body must not reach upstream")
	}
}

func TestImagesAllowsBodiesLargerThanTextLimit(t *testing.T) {
	upstream := &fakeImageProvider{}
	h, err := NewHandler(Options{
		DataPlaneToken:       "local-secret",
		Providers:            map[string]Provider{"openai-apikey": upstream},
		MaxRequestBytes:      32,
		ImageMaxRequestBytes: 256,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	body := `{"model":"openai-apikey/gpt-image-2","prompt":"` + strings.Repeat("x", 40) + `"}`
	if len(body) <= 32 {
		t.Fatalf("fixture too small: %d", len(body))
	}
	rr := postImages(t, h, imageGenerationsPath, body, "application/json", "Bearer local-secret")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !upstream.called {
		t.Fatal("image ceiling must be independent of the text request limit")
	}
}

func TestImagesDecompressesGzipUnderCeiling(t *testing.T) {
	upstream := &fakeImageProvider{}
	h, err := NewHandler(Options{
		DataPlaneToken:       "local-secret",
		Providers:            map[string]Provider{"openai-apikey": upstream},
		ImageMaxRequestBytes: 256,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	raw := []byte(`{"model":"openai-apikey/gpt-image-2","prompt":"gzip-cat"}`)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, imageGenerationsPath, bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(upstream.last.Body, []byte("gzip-cat")) {
		t.Fatalf("decompressed body=%s", upstream.last.Body)
	}
}

func TestImagesRejectsPrivateImageURL(t *testing.T) {
	upstream := &fakeImageProvider{}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": upstream},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	body := `{"model":"openai-apikey/gpt-image-2","prompt":"edit","images":[{"image_url":"http://127.0.0.1/secret.png"}]}`
	rr := postImages(t, h, imageEditsPath, body, "application/json", "Bearer local-secret")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if upstream.called {
		t.Fatal("private image URL must not be forwarded")
	}
}

func TestImagesClientCancelAbortsUpstream(t *testing.T) {
	block := make(chan struct{})
	upstream := &fakeImageProvider{block: block}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": upstream},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, imageGenerationsPath, strings.NewReader(`{"model":"openai-apikey/gpt-image-2","prompt":"a cat"}`))
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer local-secret")
	done := make(chan *httptest.ResponseRecorder)
	go func() {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		done <- rr
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	rr := <-done
	if rr.Code != 499 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestImagesTimeoutIsBounded(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	upstream := &fakeImageProvider{block: block}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": upstream},
		ImageTimeout:   30 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := postImages(t, h, imageGenerationsPath, `{"model":"openai-apikey/gpt-image-2","prompt":"a cat"}`, "application/json", "Bearer local-secret")
	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestImagesAttributesTimelineRoute(t *testing.T) {
	store := timeline.NewStore(t.TempDir(), 8)
	upstream := &fakeImageProvider{}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": upstream},
		Timeline:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, imageGenerationsPath, strings.NewReader(`{"model":"openai-apikey/gpt-image-2","prompt":"a cat"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "req-image-1")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	got, err := store.Load("req-image-1")
	if err != nil {
		t.Fatal(err)
	}
	route := got.Route()
	if route.RequestedProvider != "openai-apikey" || route.ProviderConnection != "openai-apikey" || route.Model != "gpt-image-2" {
		t.Fatalf("route=%+v", route)
	}
}

func TestImagesBareModelPicksUniqueCapableUpstream(t *testing.T) {
	upstream := &fakeImageProvider{}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{
			"kiro":          &fakeProvider{},
			"openai-apikey": upstream,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := postImages(t, h, imageGenerationsPath, `{"model":"gpt-image-2","prompt":"a cat"}`, "application/json", "Bearer local-secret")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !upstream.called {
		t.Fatal("unique image-capable provider was not selected")
	}
}
