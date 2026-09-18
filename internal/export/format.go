package export

func FormatDocument(format Format, doc any) (string, error) {
	return serialize(format, doc)
}
