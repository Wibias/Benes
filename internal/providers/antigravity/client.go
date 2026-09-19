package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/credentialpool"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
	"github.com/Wibias/Benes/internal/transport"
)

type Config struct {
	Endpoint          string
	Accounts          []Account
	CatalogModels     []string
	HTTPClient        *http.Client
	AuthHTTPClient    *http.Client
	Authority         AccountAuthority
	DestinationPolicy transport.DestinationPolicy
	TransportOptions  transport.ClientOptions
	ImageGeneration   bool
	Discover          func(ctx context.Context, token string) (string, error)
	Credentials       *credentialpool.Pool
	Continuation      *continuation.Authority
}

type Client struct {
	endpoint     string
	accounts     []Account
	catalog      map[string]struct{}
	httpClient   *http.Client
	pool         *Pool
	image        bool
	discover     func(ctx context.Context, token string) (string, error)
	credentials  *credentialpool.Pool
	destination  string
	continuation *continuation.Authority
	authority    AccountAuthority
}

func NewHardened(ctx context.Context, config Config) (*Client, error) {
	endpoint, err := ResolveDestination(config.Endpoint)
	if err != nil {
		return nil, err
	}
	if config.HTTPClient == nil {
		target, err := transport.ResolveTarget(ctx, endpoint, config.DestinationPolicy)
		if err != nil {
			return nil, fmt.Errorf("validate Cloud Code Assist destination: %w", err)
		}
		config.HTTPClient = transport.NewClient(target, config.TransportOptions)
	}
	authority := config.Authority
	if authority == nil {
		if sourcePath := commonAccountSourcePath(config.Accounts); sourcePath != "" {
			authClient := config.AuthHTTPClient
			if authClient == nil {
				authClient = transport.NewUnpinnedClient(config.TransportOptions)
			}
			authority = NewFileAccountAuthority(sourcePath, authClient, config.Discover)
		}
	}
	allowed := map[string]struct{}{}
	for _, model := range config.CatalogModels {
		id := strings.TrimSpace(model)
		if id == "" {
			continue
		}
		if _, name, ok := strings.Cut(id, "/"); ok {
			id = name
		}
		allowed[id] = struct{}{}
	}
	pool := NewPool()
	client := &Client{
		endpoint:     endpoint,
		destination:  endpoint,
		accounts:     append([]Account(nil), config.Accounts...),
		catalog:      allowed,
		httpClient:   config.HTTPClient,
		pool:         pool,
		image:        config.ImageGeneration,
		discover:     config.Discover,
		credentials:  config.Credentials,
		continuation: config.Continuation,
		authority:    authority,
	}
	for _, account := range client.accounts {
		if strings.TrimSpace(account.ID) == "" {
			continue
		}
		if config.Credentials != nil {
			config.Credentials.Upsert(credentialpool.Candidate{
				Ref:           account.ID,
				ProviderLabel: "google-antigravity",
				Destination:   endpoint,
				AuthClass:     "oauth",
				Evidence:      credentialpool.Evidence{Auth: credentialpool.AuthUsable, Quota: credentialpool.QuotaUnknown, Limit: credentialpool.LimitAvailable},
			})
		}
	}
	return client, nil
}

func (c *Client) Protocol() string {
	return "google-antigravity"
}

func (c *Client) Open(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	if c == nil {
		return nil, fmt.Errorf("antigravity client is required")
	}
	var pinned *Account
	allowAccountFailover := true
	if pin := dispatch.PhysicalPin; pin != nil && strings.TrimSpace(pin.CredentialRef) != "" {
		acct, err := c.accountByID(strings.TrimSpace(pin.CredentialRef))
		if err != nil {
			return nil, err
		}
		pinned = &acct
		allowAccountFailover = false
	} else if dispatch.PreferCommitted || antigravityDispatchOwnsContinuation(dispatch) {
		acct, err := c.selectAccount(ctx, nil)
		if err != nil {
			return nil, err
		}
		pinned = &acct
		allowAccountFailover = false
	}
	endpoint := c.endpoint
	if pin := dispatch.PhysicalPin; pin != nil && strings.TrimSpace(pin.Destination) != "" {
		endpoint = strings.TrimSpace(pin.Destination)
	}
	return c.open(ctx, dispatch, endpoint, 0, false, "", pinned, allowAccountFailover)
}

func (c *Client) accountByID(id string) (Account, error) {
	for _, account := range c.accounts {
		if strings.TrimSpace(account.ID) == id {
			return account, nil
		}
	}
	return Account{}, fmt.Errorf("Cloud Code Assist pinned account unavailable")
}

func antigravityDispatchOwnsContinuation(dispatch providers.DispatchRequest) bool {
	if strings.TrimSpace(dispatch.Parsed.PreviousResponseID) != "" {
		return true
	}
	for _, msg := range dispatch.Parsed.Context.Messages {
		for _, part := range msg.Content {
			if strings.TrimSpace(part.ThoughtSignature) != "" {
				return true
			}
			if part.ProviderMetadata != nil && part.ProviderMetadata.Google != nil && strings.TrimSpace(part.ProviderMetadata.Google.ThoughtSignature) != "" {
				return true
			}
		}
	}
	return false
}

func (c *Client) open(ctx context.Context, dispatch providers.DispatchRequest, endpoint string, accountHops int, peerFailed bool, refreshedAccountID string, pinned *Account, allowAccountFailover bool) (providers.EventStream, error) {
	model := strings.TrimSpace(dispatch.Parsed.UpstreamModelID)
	if model == "" {
		model = strings.TrimSpace(dispatch.Parsed.ModelID)
	}
	if len(c.catalog) > 0 {
		if _, ok := c.catalog[model]; !ok {
			return nil, fmt.Errorf("model %q is not in the catalog authority", model)
		}
	}
	account, err := c.selectAccount(ctx, pinned)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(account.ProjectID) == "" {
		project, err := c.discoverProject(ctx, account.Token)
		if err != nil || strings.TrimSpace(project) == "" {
			return nil, fmt.Errorf("Cloud Code Assist project id is required")
		}
		account.ProjectID = project
		c.storeProject(account.ID, project)
	}
	dest, err := ResolveDestination(endpoint)
	if err != nil {
		if c.image && peerFailed {
			return nil, ImageTransportFailure(true)
		}
		// Tests pin an already-open loopback listener after NewHardened.
		dest = strings.TrimRight(endpoint, "/")
	}
	bound, err := c.bindContinuation(dispatch, account, model, dest)
	if err != nil {
		return nil, err
	}
	if bound != nil {
		defer bound.Release()
	}
	parsed := dispatch.Parsed
	if bound != nil {
		parsed = bound.Request
	}
	env, err := CompileDispatchEnvelope(parsed, account, c.image)
	if err != nil {
		return nil, err
	}
	body, err := encodeEnvelope(env)
	if err != nil {
		return nil, err
	}
	if dispatch.Turn != nil {
		if _, err := dispatch.Turn.Reserve(resourcebudget.ClassTranslator, int64(len(body))); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest+"/v1internal:streamGenerateContent?alt=sse", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+account.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", RequestUserAgent)
	if strings.Contains(strings.ToLower(model), "claude") {
		req.Header.Set("anthropic-beta", InterleavedThinkingBeta)
	}
	response, err := transport.DoPhysicalSend(ctx, c.httpClient, req, dispatch.Turn, "google-antigravity")
	if err != nil {
		if errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
			return nil, err
		}
		if c.image {
			return nil, ImageTransportFailure(true)
		}
		return nil, err
	}
	if response.StatusCode == http.StatusUnauthorized {
		_ = response.Body.Close()
		if c.image {
			return nil, ImageTransportFailure(true)
		}
		refreshFailed := false
		if refreshedAccountID != account.ID && c.authority != nil {
			fresh, refreshErr := c.authority.RefreshAfterUnauthorized(ctx, account)
			if refreshErr != nil {
				if errors.Is(refreshErr, context.Canceled) || errors.Is(refreshErr, context.DeadlineExceeded) {
					return nil, refreshErr
				}
				refreshFailed = true
			} else if strings.TrimSpace(fresh.ID) == "" || fresh.ID != account.ID || strings.TrimSpace(fresh.Token) == "" || strings.TrimSpace(fresh.ProjectID) == "" {
				refreshFailed = true
			} else {
				return c.open(ctx, dispatch, endpoint, accountHops, peerFailed, account.ID, &fresh, allowAccountFailover)
			}
		}
		c.mark(account.ID, FailureAuth, 0)
		if allowAccountFailover && accountHops < maxPreStreamFailover && c.hasSelectableAccount() && c.pool.AllowPreStreamFailover() {
			return c.open(ctx, dispatch, endpoint, accountHops+1, peerFailed, refreshedAccountID, nil, allowAccountFailover)
		}
		if refreshFailed {
			return nil, fmt.Errorf("Cloud Code Assist authentication recovery failed")
		}
		return nil, fmt.Errorf("Cloud Code Assist upstream returned HTTP %d", response.StatusCode)
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
		raw := readLimited(response.Body, 64<<10)
		_ = response.Body.Close()
		kind := ClassifyStatus(response.StatusCode, raw, response.StatusCode == http.StatusForbidden && isGeoblockBody(strings.ToLower(raw)))
		retryAfter := ParseRetryAfter(response.Header)
		c.mark(account.ID, kind, retryAfter)
		if c.image {
			return nil, ImageTransportFailure(true)
		}
		if kind != FailureGeoblock && allowAccountFailover && accountHops < maxPreStreamFailover && c.hasSelectableAccount() && c.pool.AllowPreStreamFailover() {
			return c.open(ctx, dispatch, endpoint, accountHops+1, peerFailed, refreshedAccountID, nil, allowAccountFailover)
		}
		return nil, fmt.Errorf("Cloud Code Assist account %s is %s", account.ID, kind)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = response.Body.Close()
		if c.image {
			return nil, ImageTransportFailure(true)
		}
		class := failoverClass(response.StatusCode)
		probe := NewDecoder(bytes.NewReader(nil), 1)
		if pinned == nil && !peerFailed && class != "" && c.pool.AllowPreStreamFailover() && probe.AllowPeerFailover(class) == nil {
			if peer, ok := Peer(c.destination); ok {
				return c.open(ctx, dispatch, peer, accountHops, true, refreshedAccountID, nil, allowAccountFailover)
			}
		}
		return nil, fmt.Errorf("Cloud Code Assist upstream returned HTTP %d", response.StatusCode)
	}
	decoder := NewDecoder(response.Body, 256<<10)
	first, firstErr := decoder.Next()
	if c.image && firstErr != nil && firstErr != io.EOF {
		_ = response.Body.Close()
		return nil, ImageTransportFailure(true)
	}
	if pinned == nil && !c.image && !peerFailed {
		class := FailoverEmpty
		if firstErr == nil {
			class = classifyProbedLine(first)
		}
		if (firstErr == io.EOF || class != "") && decoder.AllowPeerFailover(classOrEmpty(firstErr, class)) == nil {
			if peer, ok := Peer(c.destination); ok {
				_ = response.Body.Close()
				return c.open(ctx, dispatch, peer, accountHops, true, refreshedAccountID, nil, allowAccountFailover)
			}
		}
	}
	out := &stream{
		decoder:  decoder,
		body:     response.Body,
		turn:     dispatch.Turn,
		account:  account.ID,
		pool:     c.pool,
		pending:  first,
		firstErr: firstErr,
		physical: &providers.PhysicalPin{Destination: dest, CredentialRef: account.ID, AuthClass: "oauth"},
	}
	if bound != nil && c.continuation != nil {
		out.continuation = c.continuation
		out.owner = bound.Owner
		out.durable = bound.Durable
		out.thread = strings.TrimSpace(dispatch.Parsed.PreviousResponseID)
	}
	return out, nil
}

func (c *Client) selectAccount(ctx context.Context, pinned *Account) (Account, error) {
	if pinned != nil {
		return *pinned, nil
	}
	account, err := c.pool.Select(c.accounts)
	if err != nil {
		return Account{}, err
	}
	if c.authority == nil {
		return account, nil
	}
	fresh, err := c.authority.Snapshot(ctx, account.ID)
	if err != nil {
		return Account{}, fmt.Errorf("Cloud Code Assist account snapshot unavailable")
	}
	if strings.TrimSpace(fresh.ID) == "" || fresh.ID != account.ID || strings.TrimSpace(fresh.Token) == "" {
		return Account{}, fmt.Errorf("Cloud Code Assist account snapshot unavailable")
	}
	return fresh, nil
}

func (c *Client) hasSelectableAccount() bool {
	if c == nil || c.pool == nil {
		return false
	}
	_, err := c.pool.Select(c.accounts)
	return err == nil
}

func commonAccountSourcePath(accounts []Account) string {
	path := ""
	for _, account := range accounts {
		if strings.TrimSpace(account.ID) == "" || strings.TrimSpace(account.Token) == "" {
			continue
		}
		candidate := strings.TrimSpace(account.SourcePath)
		if candidate == "" {
			return ""
		}
		if path == "" {
			path = candidate
			continue
		}
		if path != candidate {
			return ""
		}
	}
	return path
}

func (c *Client) bindContinuation(dispatch providers.DispatchRequest, account Account, model, destination string) (*continuation.Bound, error) {
	if c == nil || c.continuation == nil {
		return nil, nil
	}
	if strings.TrimSpace(model) == "" {
		model = "google-antigravity"
	}
	if strings.TrimSpace(destination) == "" {
		destination = c.destination
	}
	bound, err := c.continuation.Bind(continuation.BindRequest{
		Identity: continuation.PhysicalIdentity{
			Provider:      "google-antigravity",
			Destination:   destination,
			Adapter:       "google-antigravity",
			Model:         model,
			AuthClass:     "oauth",
			Secret:        []byte(account.Token),
			AccountHandle: strings.TrimSpace(account.ID),
		},
		Thread:              strings.TrimSpace(dispatch.Parsed.PreviousResponseID),
		Turn:                dispatch.Turn,
		Request:             dispatch.Parsed,
		StripPreviousOnMiss: true,
	})
	if err != nil {
		return nil, err
	}
	return &bound, nil
}

func (c *Client) discoverProject(ctx context.Context, token string) (string, error) {
	if c.discover != nil {
		return c.discover(ctx, token)
	}
	return DiscoverProject(ctx, c.httpClient, c.destination, token)
}

func (c *Client) storeProject(accountID, project string) {
	for i := range c.accounts {
		if c.accounts[i].ID == accountID {
			c.accounts[i].ProjectID = project
		}
	}
}

func (c *Client) mark(accountID string, kind FailureKind, retryAfter time.Duration) {
	c.pool.Mark(accountID, kind, retryAfter)
	if c.credentials == nil {
		return
	}
	class := credentialpool.FailureRateLimited
	switch kind {
	case FailureQuota:
		class = credentialpool.FailureQuotaExhausted
	case FailureGeoblock:
		class = credentialpool.FailureGeoBlocked
	case FailurePermission:
		class = credentialpool.FailurePermissionDenied
	case FailureAuth:
		class = credentialpool.FailureAuthUnusable
	}
	retryAt := time.Time{}
	if retryAfter > 0 {
		retryAt = time.Now().Add(retryAfter)
	}
	c.credentials.ReportFailure(accountID, credentialpool.Failure{Class: class, RetryAt: retryAt})
}

type stream struct {
	decoder      *Decoder
	body         io.Closer
	turn         *resourcebudget.Turn
	account      string
	pool         *Pool
	pending      string
	firstErr     error
	started      bool
	continuation *continuation.Authority
	owner        continuation.Owner
	durable      bool
	thread       string
	physical       *providers.PhysicalPin
	pendingToolEnd bool
	pendingTool    protocol.Event
}

func (s *stream) Next() (protocol.Event, error) {
	if s != nil && s.pendingToolEnd {
		s.pendingToolEnd = false
		ev := s.pendingTool
		s.pendingTool = protocol.Event{}
		return ev, nil
	}
	for {
		var line string
		var err error
		if !s.started {
			s.started = true
			line, err = s.pending, s.firstErr
		} else {
			line, err = s.decoder.Next()
		}
		if err != nil {
			return protocol.Event{}, err
		}
		if s.turn != nil {
			if _, err := s.turn.Reserve(resourcebudget.ClassStreamPending, int64(len(line))); err != nil {
				return protocol.Event{}, err
			}
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		frame := parseCCAFrame(payload)
		if frame.done {
			return protocol.Event{}, io.EOF
		}
		if frame.errText != "" {
			if s.pool != nil && s.account != "" {
				s.pool.Mark(s.account, frame.errKind, 0)
			}
			return protocol.Event{Type: protocol.EventError, Message: string(frame.errKind)}, nil
		}
		if frame.thought != "" {
			return protocol.Event{Type: protocol.EventThinkingDelta, Thinking: frame.thought}, nil
		}
		if frame.tool != "" {
			if s.turn != nil {
				if _, err := s.turn.Reserve(resourcebudget.ClassToolArguments, int64(len(frame.args))); err != nil {
					return protocol.Event{}, err
				}
			}
			if s.continuation != nil && strings.TrimSpace(frame.callID) != "" && strings.TrimSpace(frame.signature) != "" {
				_ = s.continuation.RememberSignature(s.owner, s.durable, s.thread, frame.callID, frame.signature)
			}
			var meta json.RawMessage
			if strings.TrimSpace(frame.signature) != "" {
				meta, _ = json.Marshal(map[string]any{"google": map[string]string{"thoughtSignature": frame.signature}})
			}
			args := string(frame.args)
			s.pendingToolEnd = true
			s.pendingTool = protocol.Event{Type: protocol.EventToolCallEnd, ID: frame.callID, Name: frame.tool, Arguments: args, ProviderMetadata: meta}
			return protocol.Event{Type: protocol.EventToolCallStart, ID: frame.callID, Name: frame.tool, Arguments: args, ProviderMetadata: meta}, nil
		}
		if frame.text != "" {
			return protocol.Event{Type: protocol.EventTextDelta, Text: frame.text}, nil
		}
	}
}

func (s *stream) PhysicalOwnership() *providers.PhysicalPin {
	if s == nil || s.physical == nil {
		return nil
	}
	pin := *s.physical
	return &pin
}

func (s *stream) Close() error {
	if s == nil || s.body == nil {
		return nil
	}
	return s.body.Close()
}

func failoverClass(status int) FailoverClass {
	switch status {
	case http.StatusNotFound:
		return Failover404
	case http.StatusServiceUnavailable:
		return Failover503
	default:
		return ""
	}
}

func classifyProbedLine(line string) FailoverClass {
	if !strings.HasPrefix(line, "data:") {
		return ""
	}
	return parseCCAFrame(strings.TrimSpace(strings.TrimPrefix(line, "data:"))).kind
}

func classOrEmpty(err error, class FailoverClass) FailoverClass {
	if err == io.EOF && class == "" {
		return FailoverEmpty
	}
	if class == "" {
		return FailoverEmpty
	}
	return class
}

func readLimited(r io.Reader, max int64) string {
	if r == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(r, max))
	if err != nil {
		return ""
	}
	return string(raw)
}

func firstText(root map[string]json.RawMessage) string {
	for _, key := range []string{"text", "output"} {
		var text string
		if json.Unmarshal(root[key], &text) == nil && text != "" {
			return text
		}
	}
	return ""
}
