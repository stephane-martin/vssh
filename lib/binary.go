package lib

var nullChar = byte(0)
var backspaceChar = byte(8)
var crChar = byte(13)
var subsChar = byte(26)

func isControlChar(ch byte) bool {
	return (ch > nullChar && ch < backspaceChar) || (ch > crChar && ch < subsChar)
}

func IsBinary(content []byte) bool {
	if len(content) >= 8000 {
		content = content[:8000]
	}
	for _, c := range content {
		if isControlChar(c) {
			return true
		}
	}
	return false
}
