package nativemain

const (
	maxAuthBytes       = 4 * 1024 * 1024
	maxVaultBytes      = 4 * 1024 * 1024
	maxJournalBytes    = 17 * 1024 * 1024
	maxNativeProfiles  = 32
	domainHome         = "benes-native-profile-home-v1\x00"
	domainInstance     = "benes-native-profile-instance-v1\x00"
	domainIdentity     = "benes-native-profile-identity-v1\x00"
	keyringService     = "benes.native-main-profile.v1"
	sharedMetadataDir  = ".benes-native-main-profiles"
	instanceStagingDir = "native-main-profile-staging"
	legacyMetadataDir  = "native-main-profiles"
	stageLeaseMS       = 30 * 60 * 1000
	stageHeartbeatMS   = 60 * 1000
	tokenDomain        = "benes-native-stage-writer-v1\x00"
)

type Envelope struct {
	Cipher         string `json:"cipher"`
	KeyRef         string `json:"keyRef"`
	Nonce          string `json:"nonce"`
	Ciphertext     string `json:"ciphertext"`
	Tag            string `json:"tag"`
	EnvelopeSHA256 string `json:"envelopeSha256"`
}

type Record struct {
	ID           string    `json:"id"`
	Label        string    `json:"label"`
	IdentityHash string    `json:"identityHash"`
	IdentityHint string    `json:"identityHint"`
	State        string    `json:"state"`
	Payload      *Envelope `json:"payload"`
	CreatedAt    string    `json:"createdAt"`
	UpdatedAt    string    `json:"updatedAt"`
}

type Vault struct {
	Version         int      `json:"version"`
	Revision        int      `json:"revision"`
	HomeID          string   `json:"homeId"`
	ActiveProfileID *string  `json:"activeProfileId"`
	Profiles        []Record `json:"profiles"`
}

type PublicProfile struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	IdentityHint string `json:"identityHint"`
	State        string `json:"state"`
}

type ListResult struct {
	EffectiveCodexHome string          `json:"effectiveCodexHome"`
	ActiveProfileID    *string         `json:"activeProfileId"`
	Profiles           []PublicProfile `json:"profiles"`
}

type AuthSnapshot struct {
	Text      string
	Digest    string
	AccountID string
	Raw       []byte
}

type Key struct {
	Ref string
	Raw []byte
}

type KeyProvider interface {
	Get(homeID string) (*Key, error)
	Create(homeID string) (*Key, error)
}

type Journal struct {
	Version            int      `json:"version"`
	TransactionID      string   `json:"transactionId"`
	HomeID             string   `json:"homeId"`
	Phase              string   `json:"phase"`
	SourceProfileID    string   `json:"sourceProfileId"`
	SourceIdentityHash string   `json:"sourceIdentityHash"`
	SourcePayload      Envelope `json:"sourcePayload"`
	TargetProfileID    string   `json:"targetProfileId"`
	TargetIdentityHash string   `json:"targetIdentityHash"`
	TargetPayload      Envelope `json:"targetPayload"`
	BeforeVault        Vault    `json:"beforeVault"`
	AfterVault         Vault    `json:"afterVault"`
	CreatedAt          string   `json:"createdAt"`
}

type StageRecord struct {
	StageID           string `json:"stageId"`
	LeaseID           string `json:"leaseId"`
	WriterTokenHash   string `json:"writerTokenHash"`
	CreatorInstanceID string `json:"creatorInstanceId"`
	StagingRoot       string `json:"stagingRoot"`
	State             string `json:"state"`
	CreatedAt         int64  `json:"createdAt"`
	LastHeartbeatAt   int64  `json:"lastHeartbeatAt"`
	LeaseExpiresAt    int64  `json:"leaseExpiresAt"`
}

type StageRegistry struct {
	Version  int           `json:"version"`
	Revision int           `json:"revision"`
	HomeID   string        `json:"homeId"`
	Stages   []StageRecord `json:"stages"`
}
