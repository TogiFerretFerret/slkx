package emoji

import (
	"reflect"
	"testing"
)

// text builds a TokenText for table-driven test brevity.
func text(s string) Token { return Token{Kind: TokenText, Text: s} }

// emoji builds a TokenEmoji for table-driven test brevity.
func emoji(plain, url string) Token { return Token{Kind: TokenEmoji, Text: plain, URL: url} }

func TestResolveEmojiToTokens_Trivial(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Token
	}{
		{"empty", "", nil},
		{"ascii only", "hello world", []Token{text("hello world")}},
		{"only spaces", "   ", []Token{text("   ")}},
		{"newlines preserved", "line one\nline two", []Token{text("line one\nline two")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveEmojiToTokens(c.in, nil)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ResolveEmojiToTokens(%q) = %#v\n want %#v", c.in, got, c.want)
			}
		})
	}
}
