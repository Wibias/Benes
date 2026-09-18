package sessions

import "time"

type Summary struct {
	ID             string
	Namespace      string
	ExternalID     string
	StartedAt      time.Time
	LastActivityAt time.Time
	RequestCount   int
	Protocols      []string
}

type Routing struct {
	Kind              string
	RequestedModel    string
	RequestedProvider string
	ResolvedModel     string
	Provider          string
	ComboID           string
	PolicyID          string
	CommittedMember   string
}

type Attempt struct {
	Member   string `json:"member,omitempty"`
	Status   int    `json:"status,omitempty"`
	Code     string `json:"code,omitempty"`
	Decision string `json:"decision,omitempty"`
}

type Usage struct {
	InputTokens       *int64
	CachedInputTokens *int64
	OutputTokens      *int64
	TotalTokens       *int64
	Cost              *float64
	Currency          string
}

type Request struct {
	ID            string
	SessionID     string
	CorrelationID string
	StartedAt     time.Time
	Protocol      string
	Method        string
	Path          string
	Status        int
	DurationMs    int64
	RequestBytes  *int64
	ResponseBytes *int64
	Routing       Routing
	Attempts      []Attempt
	Usage         *Usage
}

type RecordInput struct {
	RequestID          string
	CorrelationID      string
	StartedAt          time.Time
	Protocol           string
	Method             string
	Path               string
	Status             int
	Duration           time.Duration
	RequestBytes       *int64
	ResponseBytes      *int64
	Identity           Identity
	PreviousResponseID string
	OutgoingResponseID string
	SeedResponsesChain bool
	Routing            Routing
	Attempts           []Attempt
	Usage              *Usage
}

type ListOptions struct {
	Limit     int
	Cursor    string
	Q         string
	Namespace string
	Protocol  string
	Provider  string
	Model     string
	Policy    string
	Combo     string
}

type ListResult struct {
	Sessions   []Summary
	HasMore    bool
	NextCursor string
}

type DetailOptions struct {
	Limit  int
	Cursor string
}

type Detail struct {
	Session    Summary
	Requests   []Request
	Aggregates Aggregates
	HasMore    bool
	NextCursor string
}

type FieldCoverage struct {
	Value              *int64
	AttributedRequests int
	TotalRequests      int
	Complete           bool
}

type CostCoverage struct {
	Value              *float64
	Currency           string
	Currencies         []string
	AttributedRequests int
	TotalRequests      int
	Complete           bool
}

type UsageAggregate struct {
	InputTokens       FieldCoverage
	CachedInputTokens FieldCoverage
	OutputTokens      FieldCoverage
	TotalTokens       FieldCoverage
	Cost              CostCoverage
}

type Aggregates struct {
	Usage                UsageAggregate
	Protocols            []string
	Models               []string
	Providers            []string
	PolicyIDs            []string
	ComboIDs             []string
	FailoverRequestCount int
	HadFailover          bool
}

type FilterValues struct {
	Protocols  []string
	Namespaces []string
	Providers  []string
	Models     []string
	PolicyIDs  []string
	ComboIDs   []string
}
