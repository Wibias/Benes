package cursor

import (
	"errors"
	"sync"

	"github.com/Wibias/Benes/internal/resourcebudget"
)

const (
	maxReplayRoots = 4096
	maxReplayBytes = 8 << 20 // assembled root+turn blob payload, including turn == nil
)

var (
	ErrReplayRootLimit = errors.New("cursor replay root count exceeded")
	ErrReplayByteLimit = errors.New("cursor replay envelope bytes exceeded")
)

type BlobStore struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func NewBlobStore() *BlobStore {
	return &BlobStore{blobs: map[string][]byte{}}
}

func (s *BlobStore) Put(data []byte) []byte {
	if s == nil {
		return BlobID(data)
	}
	id := BlobID(data)
	s.mu.Lock()
	s.blobs[string(id)] = append([]byte(nil), data...)
	s.mu.Unlock()
	return id
}

func (s *BlobStore) IDs() [][]byte {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([][]byte, 0, len(s.blobs))
	for id := range s.blobs {
		ids = append(ids, []byte(id))
	}
	return ids
}

func (s *BlobStore) Delete(id []byte) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.blobs, string(id))
	s.mu.Unlock()
}

func (s *BlobStore) Get(id []byte) ([]byte, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.blobs[string(id)]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), data...), true
}

func (s *BlobStore) PutRoots(req RunRequest) error {
	return s.PutConversationTurn(req, nil)
}

func (s *BlobStore) PutConversation(req RunRequest) error {
	return s.PutConversationTurn(req, nil)
}

func (s *BlobStore) PutConversationTurn(req RunRequest, turn *resourcebudget.Turn) error {
	if err := checkReplayEnvelope(req); err != nil {
		return err
	}
	for _, blob := range RootPromptBlobs(req) {
		if err := s.putReserved(blob, turn); err != nil {
			return err
		}
	}
	for _, blob := range flattenConversationBlobs(req) {
		if err := s.putReserved(blob, turn); err != nil {
			return err
		}
	}
	return nil
}

func checkReplayEnvelope(req RunRequest) error {
	roots := RootPromptBlobs(req)
	if len(roots) > maxReplayRoots {
		return ErrReplayRootLimit
	}
	var total int64
	for _, blob := range roots {
		total += int64(len(blob))
		if total > maxReplayBytes {
			return ErrReplayByteLimit
		}
	}
	for _, blob := range flattenConversationBlobs(req) {
		total += int64(len(blob))
		if total > maxReplayBytes {
			return ErrReplayByteLimit
		}
	}
	return nil
}

func (s *BlobStore) putReserved(data []byte, turn *resourcebudget.Turn) error {
	if turn != nil {
		if _, err := turn.Reserve(resourcebudget.ClassBlob, int64(len(data))); err != nil {
			return err
		}
	}
	s.Put(data)
	return nil
}

func ParseGetBlobArgs(raw []byte) (id uint32, blobID []byte, ok bool) {
	root, err := decodeProtoFields(raw)
	if err != nil {
		return 0, nil, false
	}
	kv := fieldBytes(root, 4)
	if len(kv) == 0 {
		return 0, nil, false
	}
	fields, err := decodeProtoFields(kv)
	if err != nil {
		return 0, nil, false
	}
	reqID, _ := fieldVarint(fields, 1)
	args := fieldBytes(fields, 2)
	if len(args) == 0 {
		return 0, nil, false
	}
	argFields, err := decodeProtoFields(args)
	if err != nil {
		return 0, nil, false
	}
	blobID = fieldBytes(argFields, 1)
	if len(blobID) == 0 {
		return 0, nil, false
	}
	return uint32(reqID), blobID, true
}

func EncodeGetBlobResult(id uint32, data []byte) []byte {
	result := EncodeProtoBytes(1, data)
	msg := EncodeProtoVarint(1, uint64(id))
	msg = append(msg, EncodeProtoMessage(2, result)...)
	return EncodeProtoMessage(3, msg)
}
