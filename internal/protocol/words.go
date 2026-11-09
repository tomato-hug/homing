package protocol

import (
	_ "embed"
	"strings"
)

//go:embed bip39-chinese-simplified.txt
var bip39WordText string

var bip39Words = func() [2048]string {
	var words [2048]string
	lines := strings.Split(strings.TrimSpace(bip39WordText), "\n")
	if len(lines) != len(words) {
		panic("protocol: BIP39 Chinese word list must contain 2048 entries")
	}
	for index, line := range lines {
		words[index] = strings.TrimSpace(line)
	}
	return words
}()
