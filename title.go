package main

import (
	"bytes"
	"strings"
)

// titleDecoration is what carts commonly wrap their title in, such as Celeste's
// "-- ~celeste~", and is trimmed from both ends of it.
const titleDecoration = "~*=+ \t"

// cartTitle returns the title of a cart from its UTF-8 Lua source. The BBS
// convention, which PICO-8 itself follows when it draws the cart label, puts the
// title in a comment on the very first line of code, so that is the only line
// read: a comment further down is ordinary code commentary and not a title.
// Carts write the comment both as "-- title" and as "--title".
//
// The first line is not a title when it is code, opens a block comment, holds
// nothing but decoration, or is an author line such as "-- by someone", which
// carts with no title of their own still begin with.
func cartTitle(lua []byte) (string, bool) {
	first, _, _ := bytes.Cut(lua, []byte("\n"))
	line := strings.TrimSuffix(string(first), "\r")

	rest, ok := strings.CutPrefix(line, "--")
	if !ok {
		return "", false
	}
	if strings.HasPrefix(rest, "[[") || strings.HasPrefix(rest, "[=") {
		return "", false
	}

	title := strings.Trim(rest, titleDecoration)
	if title == "" || strings.HasPrefix(strings.ToLower(title), "by ") {
		return "", false
	}
	return title, true
}
