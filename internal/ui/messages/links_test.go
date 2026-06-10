package messages

import (
	"reflect"
	"testing"
)

func TestExtractLinks(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []Link
	}{
		{
			name: "no links",
			text: "just some text with <@U123> and <#C123|general>",
			want: nil,
		},
		{
			name: "bare link",
			text: "see <https://example.com/page>",
			want: []Link{{URL: "https://example.com/page"}},
		},
		{
			name: "labeled link",
			text: "see <https://example.com/page|the docs>",
			want: []Link{{URL: "https://example.com/page", Label: "the docs"}},
		},
		{
			name: "mixed, order of appearance",
			text: "<https://a.example/1|first> then <https://b.example/2>",
			want: []Link{
				{URL: "https://a.example/1", Label: "first"},
				{URL: "https://b.example/2"},
			},
		},
		{
			name: "duplicate URLs deduped",
			text: "<https://a.example/1> and again <https://a.example/1|same>",
			want: []Link{{URL: "https://a.example/1"}},
		},
		{
			name: "mailto",
			text: "<mailto:foo@example.com|foo@example.com>",
			want: []Link{{URL: "mailto:foo@example.com", Label: "foo@example.com"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractLinks(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ExtractLinks(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
		})
	}
}
