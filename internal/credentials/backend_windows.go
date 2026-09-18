//go:build windows

package credentials

import (
	"runtime"
	"syscall"
	"unsafe"
)

const (
	credTargetPrefix = "Benes/"
	credTypeGeneric  = 1
	credPersistLocal = 2
)

var (
	advapi32        = syscall.NewLazyDLL("advapi32.dll")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

type nativeCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        uint64
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type nativeBackend struct{}

func defaultBackend() Backend { return nativeBackend{} }

func (nativeBackend) Available() error {
	if err := procCredWriteW.Find(); err != nil {
		return ErrSecureStoreUnavailable
	}
	if err := procCredReadW.Find(); err != nil {
		return ErrSecureStoreUnavailable
	}
	return nil
}

func credTarget(id string) string { return credTargetPrefix + id }

func (nativeBackend) Put(id string, secret []byte) error {
	target, err := syscall.UTF16PtrFromString(credTarget(id))
	if err != nil {
		return ErrCredentialUnavailable
	}
	cred := nativeCredential{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(secret)),
		Persist:            credPersistLocal,
	}
	if len(secret) > 0 {
		cred.CredentialBlob = &secret[0]
	}
	r1, _, _ := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
	runtime.KeepAlive(secret)
	runtime.KeepAlive(target)
	if r1 == 0 {
		return ErrSecureStoreUnavailable
	}
	return nil
}

func (b nativeBackend) Get(id string) ([]byte, error) {
	return readSecretUntilPresent(func() ([]byte, error) {
		return b.credReadOnce(id)
	}, nil, secureStoreReadAttempts, secureStoreSleep)
}

func (nativeBackend) credReadOnce(id string) ([]byte, error) {
	target, err := syscall.UTF16PtrFromString(credTarget(id))
	if err != nil {
		return nil, ErrCredentialUnavailable
	}
	var cred *nativeCredential
	r1, _, _ := procCredReadW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&cred)))
	runtime.KeepAlive(target)
	if r1 == 0 || cred == nil {
		return nil, ErrCredentialUnavailable
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))
	if cred.CredentialBlobSize == 0 || cred.CredentialBlob == nil {
		return nil, ErrCredentialUnavailable
	}
	out := unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)
	return append([]byte(nil), out...), nil
}

func (nativeBackend) Delete(id string) error {
	target, err := syscall.UTF16PtrFromString(credTarget(id))
	if err != nil {
		return nil
	}
	procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	return nil
}
