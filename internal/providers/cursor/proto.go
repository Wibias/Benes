package cursor

func appendVarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func EncodeProtoString(field int, value string) []byte {
	return EncodeProtoBytes(field, []byte(value))
}

func EncodeProtoBytes(field int, value []byte) []byte {
	out := appendVarint(nil, uint64(field<<3|2))
	out = appendVarint(out, uint64(len(value)))
	return append(out, value...)
}

func EncodeProtoMessage(field int, msg []byte) []byte {
	return EncodeProtoBytes(field, msg)
}

func EncodeProtoVarint(field int, value uint64) []byte {
	out := appendVarint(nil, uint64(field<<3))
	return appendVarint(out, value)
}

type protoField struct {
	num    int
	wire   int
	bytes  []byte
	varint uint64
}

func decodeProtoFields(raw []byte) ([]protoField, error) {
	var fields []protoField
	i := 0
	for i < len(raw) {
		key, n := consumeVarint(raw[i:])
		if n == 0 {
			return nil, errProtoTruncated
		}
		i += n
		num := int(key >> 3)
		wire := int(key & 7)
		switch wire {
		case 0:
			v, m := consumeVarint(raw[i:])
			if m == 0 {
				return nil, errProtoTruncated
			}
			i += m
			fields = append(fields, protoField{num: num, wire: wire, varint: v})
		case 2:
			ln, m := consumeVarint(raw[i:])
			if m == 0 {
				return nil, errProtoTruncated
			}
			i += m
			if i+int(ln) > len(raw) {
				return nil, errProtoTruncated
			}
			fields = append(fields, protoField{num: num, wire: wire, bytes: raw[i : i+int(ln)]})
			i += int(ln)
		default:
			return nil, errProtoWire
		}
	}
	return fields, nil
}

func consumeVarint(raw []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(raw) && i < 10; i++ {
		b := raw[i]
		v |= uint64(b&0x7f) << (7 * i)
		if b < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}

func fieldBytes(fields []protoField, num int) []byte {
	for _, field := range fields {
		if field.num == num && field.wire == 2 {
			return field.bytes
		}
	}
	return nil
}

func fieldVarint(fields []protoField, num int) (uint64, bool) {
	for _, field := range fields {
		if field.num == num && field.wire == 0 {
			return field.varint, true
		}
	}
	return 0, false
}
