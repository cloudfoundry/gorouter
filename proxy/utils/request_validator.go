package utils

var validHostSpecialChars = map[byte]bool{
	'-':  true,
	'.':  true,
	'_':  true,
	':':  true,
	',':  true,
	'*':  true,
	'+':  true,
	'=':  true,
	'~':  true,
	'!':  true,
	'%':  true,
	'(':  true,
	')':  true,
	'$':  true,
	'&':  true,
	';':  true,
	'[':  true,
	']':  true,
	'\'': true,
}

// checks for valid host characters (not format)
func ValidHost(host string) bool {
	if len(host) == 0 {
		return false
	}

	for i := 0; i < len(host); i++ {
		c := host[i]
		if !isHostCharacterAllowed(c) {
			return false
		}
	}
	return true
}

func alphaNumeric(c byte) bool {
	return ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z') ||
		('0' <= c && c <= '9')
}

func isHostCharacterAllowed(c byte) bool {
	/* To check valid host characters, refer to:
	   - Section 3.1 of RFC 1738
	   - Section 3.5 of RFC 1034
	   - Section 2.1 of RFC 1123 */
	return alphaNumeric(c) || validHostSpecialChars[c]
}
