package stemmer

import (
	"strings"
)

// PorterStemmer implements Martin Porter's 1980 English Stemming Algorithm from scratch.
// It removes common morphological and inflectional endings from English words.

type porterStemmer struct {
	b []rune
	i int // offset into b
	j int // offset into b (used during step rules)
}

func isConsonant(b []rune, i int) bool {
	switch b[i] {
	case 'a', 'e', 'i', 'o', 'u':
		return false
	case 'y':
		if i == 0 {
			return true
		}
		return !isConsonant(b, i-1)
	default:
		return true
	}
}

// m measures the number of consonant sequences between 0 and j
func (s *porterStemmer) m() int {
	n := 0
	i := 0
	j := s.j

	for {
		if i > j {
			return n
		}
		if !isConsonant(s.b, i) {
			break
		}
		i++
	}
	i++
	for {
		for {
			if i > j {
				return n
			}
			if isConsonant(s.b, i) {
				break
			}
			i++
		}
		i++
		n++
		for {
			if i > j {
				return n
			}
			if !isConsonant(s.b, i) {
				break
			}
			i++
		}
		i++
	}
}

// vowelInStem returns true if 0..j contains a vowel
func (s *porterStemmer) vowelInStem() bool {
	for i := 0; i <= s.j; i++ {
		if !isConsonant(s.b, i) {
			return true
		}
	}
	return false
}

// doubleC returns true if j and j-1 are double consonants (e.g. tt, ss)
func (s *porterStemmer) doubleC(i int) bool {
	if i < 1 {
		return false
	}
	if s.b[i] != s.b[i-1] {
		return false
	}
	return isConsonant(s.b, i)
}

// cvc returns true if i-2, i-1, i has the form consonant-vowel-consonant and the second c is not w, x, or y
func (s *porterStemmer) cvc(i int) bool {
	if i < 2 || !isConsonant(s.b, i) || isConsonant(s.b, i-1) || !isConsonant(s.b, i-2) {
		return false
	}
	ch := s.b[i]
	if ch == 'w' || ch == 'x' || ch == 'y' {
		return false
	}
	return true
}

func (s *porterStemmer) ends(str string) bool {
	targetRunes := []rune(str)
	l := len(targetRunes)
	if s.i < 0 || s.i >= len(s.b) || l > s.i+1 || s.i-l+1 < 0 {
		return false
	}
	if s.b[s.i] != targetRunes[l-1] {
		return false
	}
	if string(s.b[s.i-l+1:s.i+1]) != str {
		return false
	}
	s.j = s.i - l
	return true
}

func (s *porterStemmer) setSuffix(str string) {
	runes := []rune(str)
	prefix := append([]rune(nil), s.b[:s.j+1]...)
	s.b = append(prefix, runes...)
	s.i = len(s.b) - 1
}

func (s *porterStemmer) r(str string) {
	if s.m() > 0 {
		s.setSuffix(str)
	}
}

func (s *porterStemmer) step1() {
	if s.i < 0 || s.i >= len(s.b) {
		return
	}
	if s.b[s.i] == 's' {
		if s.ends("sses") {
			s.i -= 2
		} else if s.ends("ies") {
			s.setSuffix("i")
		} else if s.i > 0 && s.b[s.i-1] != 's' {
			s.i--
		}
	}
	if s.ends("eed") {
		if s.m() > 0 {
			s.i--
		}
	} else if (s.ends("ed") || s.ends("ing")) && s.vowelInStem() {
		s.i = s.j
		if s.ends("at") {
			s.setSuffix("ate")
		} else if s.ends("bl") {
			s.setSuffix("ble")
		} else if s.ends("iz") {
			s.setSuffix("ize")
		} else if s.doubleC(s.i) {
			s.i--
			ch := s.b[s.i]
			if ch == 'l' || ch == 's' || ch == 'z' {
				s.i++
			}
		} else if s.m() == 1 && s.cvc(s.i) {
			s.setSuffix("e")
		}
	}
}

func (s *porterStemmer) step2() {
	if s.ends("y") && s.vowelInStem() {
		s.b[s.i] = 'i'
	}
}

func (s *porterStemmer) step3() {
	if s.i <= 0 {
		return
	}
	switch s.b[s.i-1] {
	case 'a':
		if s.ends("ational") {
			s.r("ate")
		} else if s.ends("tional") {
			s.r("tion")
		}
	case 'c':
		if s.ends("enci") {
			s.r("ence")
		} else if s.ends("anci") {
			s.r("ance")
		}
	case 'e':
		if s.ends("izer") {
			s.r("ize")
		}
	case 'l':
		if s.ends("bli") {
			s.r("ble")
		} else if s.ends("alli") {
			s.r("al")
		} else if s.ends("entli") {
			s.r("ent")
		} else if s.ends("eli") {
			s.r("e")
		} else if s.ends("ousli") {
			s.r("ous")
		}
	case 'o':
		if s.ends("ization") {
			s.r("ize")
		} else if s.ends("ation") {
			s.r("ate")
		} else if s.ends("ator") {
			s.r("ate")
		}
	case 's':
		if s.ends("alism") {
			s.r("al")
		} else if s.ends("iveness") {
			s.r("ive")
		} else if s.ends("fulness") {
			s.r("ful")
		} else if s.ends("ousness") {
			s.r("ous")
		}
	case 't':
		if s.ends("aliti") {
			s.r("al")
		} else if s.ends("iviti") {
			s.r("ive")
		} else if s.ends("biliti") {
			s.r("ble")
		}
	}
}

func (s *porterStemmer) step4() {
	switch s.b[s.i] {
	case 'e':
		if s.ends("icate") {
			s.r("ic")
		} else if s.ends("ative") {
			s.r("")
		} else if s.ends("alize") {
			s.r("al")
		}
	case 'i':
		if s.ends("iciti") {
			s.r("ic")
		}
	case 'l':
		if s.ends("ical") {
			s.r("ic")
		} else if s.ends("ful") {
			s.r("")
		}
	case 's':
		if s.ends("ness") {
			s.r("")
		}
	}
}

func (s *porterStemmer) step5() {
	if s.i <= 0 {
		return
	}
	switch s.b[s.i-1] {
	case 'a':
		if s.ends("al") {
		} else {
			return
		}
	case 'c':
		if s.ends("ance") || s.ends("ence") {
		} else {
			return
		}
	case 'e':
		if s.ends("er") {
		} else {
			return
		}
	case 'i':
		if s.ends("ic") {
		} else {
			return
		}
	case 'l':
		if s.ends("able") || s.ends("ible") {
		} else {
			return
		}
	case 'n':
		if s.ends("ant") || s.ends("ement") || s.ends("ment") || s.ends("ent") {
		} else {
			return
		}
	case 'o':
		if s.ends("ion") && s.j >= 0 && (s.b[s.j] == 's' || s.b[s.j] == 't') {
		} else if s.ends("ou") {
		} else {
			return
		}
	case 's':
		if s.ends("ism") {
		} else {
			return
		}
	case 't':
		if s.ends("ate") || s.ends("iti") {
		} else {
			return
		}
	case 'u':
		if s.ends("ous") {
		} else {
			return
		}
	case 'v':
		if s.ends("ive") {
		} else {
			return
		}
	case 'z':
		if s.ends("ize") {
		} else {
			return
		}
	default:
		return
	}
	if s.m() > 1 {
		s.i = s.j
	}
}

func (s *porterStemmer) step6() {
	s.j = s.i
	if s.i < 0 {
		return
	}
	if s.b[s.i] == 'e' {
		a := s.m()
		if a > 1 || (a == 1 && !s.cvc(s.i-1)) {
			s.i--
		}
	}
	if s.i >= 0 && s.b[s.i] == 'l' && s.doubleC(s.i) && s.m() > 1 {
		s.i--
	}
}

// Stem returns the stemmed form of an English word.
func Stem(word string) string {
	word = strings.TrimSpace(strings.ToLower(word))
	if len(word) <= 2 {
		return word
	}

	runes := []rune(word)
	s := &porterStemmer{
		b: runes,
		i: len(runes) - 1,
	}

	if s.i < 0 || s.i >= len(s.b) {
		return word
	}
	s.step1()
	if s.i >= 0 && s.i < len(s.b) {
		s.step2()
	}
	if s.i >= 0 && s.i < len(s.b) {
		s.step3()
	}
	if s.i >= 0 && s.i < len(s.b) {
		s.step4()
	}
	if s.i >= 0 && s.i < len(s.b) {
		s.step5()
	}
	if s.i >= 0 && s.i < len(s.b) {
		s.step6()
	}

	if s.i < 0 || s.i >= len(s.b) {
		return word
	}

	return string(s.b[:s.i+1])
}
