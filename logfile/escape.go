package logfile

import "io"

const lowerHex = "0123456789abcdef"

// writeEscapedLine writes at most one logical line from input. Special bytes
// are rendered visibly, and an input newline becomes a literal `\n` followed
// by the trusted physical newline used to delimit stored log records.
func writeEscapedLine(output io.Writer, input []byte) (consumed int, complete bool, err error) {
	plainStart := 0
	for index, value := range input {
		escaped, special := logEscape(value)
		if !special {
			continue
		}

		if plainStart < index {
			written, writeErr := output.Write(input[plainStart:index])
			consumed += written
			if writeErr != nil {
				return consumed, false, writeErr
			}
			if written != index-plainStart {
				return consumed, false, io.ErrShortWrite
			}
		}

		written, writeErr := io.WriteString(output, escaped)
		if writeErr != nil {
			return consumed, false, writeErr
		}
		if written != len(escaped) {
			return consumed, false, io.ErrShortWrite
		}
		consumed++
		plainStart = index + 1
		if value == '\n' {
			return consumed, true, nil
		}
	}

	if plainStart < len(input) {
		written, writeErr := output.Write(input[plainStart:])
		consumed += written
		if writeErr != nil {
			return consumed, false, writeErr
		}
		if written != len(input)-plainStart {
			return consumed, false, io.ErrShortWrite
		}
	}
	return consumed, false, nil
}

func logEscape(value byte) (string, bool) {
	switch value {
	case '\a':
		return `\a`, true
	case '\b':
		return `\b`, true
	case '\t':
		return `\t`, true
	case '\n':
		return "\\n\n", true
	case '\v':
		return `\v`, true
	case '\f':
		return `\f`, true
	case '\r':
		return `\r`, true
	case '"':
		return `\"`, true
	case '\\':
		return `\\`, true
	default:
		if value < 0x20 || value == 0x7f {
			return string([]byte{'\\', 'x', lowerHex[value>>4], lowerHex[value&0x0f]}), true
		}
		return "", false
	}
}
