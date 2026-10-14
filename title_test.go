package main

import "testing"

const freecell = "freecell"

func TestCartTitle(t *testing.T) {
	tests := []struct {
		name   string
		lua    string
		want   string
		wantOK bool
	}{
		{"plain title", "-- freecell\nx=1\n", freecell, true},
		{"decoration trimmed", "-- ~celeste~\n-- matt thorson + noel berry\n", "celeste", true},
		{"no space after the dashes", "--freecell\n", freecell, true},
		{"carriage return", "-- freecell\r\nx=1\r\n", freecell, true},
		{"only line", "-- freecell", freecell, true},
		{"author on the second line", "--watermelon game for pico8\n--@spoike\n\n\n--[[\n", "watermelon game for pico8", true},
		{"code after a blank line", "--main structure\n\ncartdata(\"2darray_marble_merger\")\n", "main structure", true},
		{"code on the first line", "debug=\"\"\nversion_id=\"V1.02\"\n\n-- convert old format memory into new format\n", "", false},
		{"comment only on the second line", "x=1\n-- freecell\n", "", false},
		{"blank first line", "\n-- freecell\n", "", false},
		{"author line", "-- by someone\nx=1\n", "", false},
		{"uppercase author line", "-- BY someone\n", "", false},
		{"block comment", "--[[ freecell\n]]\n", "", false},
		{"long block comment", "--[==[ freecell\n]==]\n", "", false},
		{"decoration only", "-- ~~~\n", "", false},
		{"dashes only", "--\nx=1\n", "", false},
		{"empty lua", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := cartTitle([]byte(tt.lua))
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("cartTitle(%q) = %q, %v, want %q, %v", tt.lua, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
