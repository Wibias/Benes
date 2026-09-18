package contextprojection

const (
	ShadowLargeMinBytes     = 32 * 1024
	ShadowDuplicateMinBytes = 8 * 1024
	LiveLargeMinBytes       = 96 * 1024
	LiveDuplicateMinBytes   = 16 * 1024
	LivePreviewHeadBytes    = 8 * 1024
	LivePreviewTailBytes    = 8 * 1024
	MaxScannedUTF8Bytes     = 64 * 1024 * 1024

	PolicyIDDuplicate = "duplicate-v1:dup16k:scan64m"
	PolicyIDRecovery  = "recovery-v1:dup16k:large96k:head8k:tail8k:scan64m"
	PolicyIDShadow    = "shadow-v1:dup8k:large32k:head8k:tail8k:scan64m"
)

func ShadowPolicy() Policy {
	return Policy{
		SchemaVersion:       1,
		Variant:             VariantRecoveryV1,
		PolicyID:            PolicyIDShadow,
		DuplicateMinBytes:   ShadowDuplicateMinBytes,
		LargeMinBytes:       ShadowLargeMinBytes,
		PreviewHeadBytes:    LivePreviewHeadBytes,
		PreviewTailBytes:    LivePreviewTailBytes,
		MaxScannedUTF8Bytes: MaxScannedUTF8Bytes,
	}
}

func DuplicatePolicy() Policy {
	return Policy{
		SchemaVersion:       1,
		Variant:             VariantDuplicateV1,
		PolicyID:            PolicyIDDuplicate,
		DuplicateMinBytes:   LiveDuplicateMinBytes,
		LargeMinBytes:       LiveLargeMinBytes,
		PreviewHeadBytes:    LivePreviewHeadBytes,
		PreviewTailBytes:    LivePreviewTailBytes,
		MaxScannedUTF8Bytes: MaxScannedUTF8Bytes,
	}
}

func RecoveryPolicy() Policy {
	return Policy{
		SchemaVersion:       1,
		Variant:             VariantRecoveryV1,
		PolicyID:            PolicyIDRecovery,
		DuplicateMinBytes:   LiveDuplicateMinBytes,
		LargeMinBytes:       LiveLargeMinBytes,
		PreviewHeadBytes:    LivePreviewHeadBytes,
		PreviewTailBytes:    LivePreviewTailBytes,
		MaxScannedUTF8Bytes: MaxScannedUTF8Bytes,
	}
}
