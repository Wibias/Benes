//go:build windows

package nativemain

import (
	"crypto/rand"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

func NewOSKeyProvider() KeyProvider { return windowsKeyProvider{} }

type windowsKeyProvider struct{}

type nativeCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

var (
	modAdvapi32   = windows.NewLazySystemDLL("advapi32.dll")
	procCredRead  = modAdvapi32.NewProc("CredReadW")
	procCredWrite = modAdvapi32.NewProc("CredWriteW")
	procCredFree  = modAdvapi32.NewProc("CredFree")
)

func (windowsKeyProvider) Get(homeID string) (*Key, error) {
	target, err := syscall.UTF16PtrFromString(keyringService + ":" + homeID)
	if err != nil {
		return nil, fail("KEYRING_UNAVAILABLE", "The native OS credential store could not be read.", 503)
	}
	var cred *nativeCredential
	r1, _, e1 := procCredRead.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&cred)))
	if r1 == 0 {
		if e1 == syscall.ERROR_NOT_FOUND {
			return nil, nil
		}
		return nil, fail("KEYRING_UNAVAILABLE", "The native OS credential store could not be read.", 503)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))
	if cred.CredentialBlobSize != 32 || cred.CredentialBlob == nil {
		return nil, fail("KEYRING_UNAVAILABLE", "The OS credential-store key is invalid.", 503)
	}
	raw := unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)
	return &Key{Ref: keyringService + ":" + homeID, Raw: append([]byte(nil), raw...)}, nil
}

func (windowsKeyProvider) Create(homeID string) (*Key, error) {
	if existing, err := (windowsKeyProvider{}).Get(homeID); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fail("KEYRING_UNAVAILABLE", "The native OS credential store could not save the profile key.", 503)
	}
	target, err := syscall.UTF16PtrFromString(keyringService + ":" + homeID)
	if err != nil {
		return nil, fail("KEYRING_UNAVAILABLE", "The native OS credential store could not save the profile key.", 503)
	}
	cred := nativeCredential{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(raw)),
		CredentialBlob:     &raw[0],
		Persist:            credPersistLocalMachine,
	}
	r1, _, _ := procCredWrite.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r1 == 0 {
		return nil, fail("KEYRING_UNAVAILABLE", "The native OS credential store could not save the profile key.", 503)
	}
	return &Key{Ref: keyringService + ":" + homeID, Raw: raw}, nil
}
