package usageledger

type openMode int

const (
	openRead openMode = iota
	openAppend
	openRecover
	openCreateExclusive
)
