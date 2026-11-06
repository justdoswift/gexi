package main

import (
	"testing"
	"unicode/utf8"
)

func TestFitPathKeepsTheLongestTail(t *testing.T) {
	// Seven points a rune, so the numbers read as characters.
	widthOf := func(text string) float32 { return float32(utf8.RuneCountInString(text)) * 7 }
	cases := []struct {
		path string
		room float32
		want string
	}{
		{"/Users/gepeng/src/gexi", 1000, "/Users/gepeng/src/gexi"},
		{"/Users/gepeng/src/gexi", 70, "…/src/gexi"},
		{"/Users/gepeng/src/gexi", 60, "…/gexi"},
		{"/Users/gepeng/src/gexi", 20, ""},
		{"~/dev/proj", 1000, "~/dev/proj"},
		{"~/dev/proj", 60, "…/proj"},
		{"gexi", 60, "gexi"},
		{"gexi", 20, ""},
	}
	for _, c := range cases {
		if got := fitPath(c.path, c.room, widthOf); got != c.want {
			t.Errorf("fitPath(%q, %v) = %q; want %q", c.path, c.room, got, c.want)
		}
	}
}
