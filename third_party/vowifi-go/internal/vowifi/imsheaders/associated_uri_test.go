package imsheaders

import "testing"

func TestPickAssociatedMSISDNPriority(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "SIP international precedes TEL international",
			header: `<tel:+447700900111>, <sip:+447700900222@ims.example;user=phone>`,
			want:   "+447700900222@ims.example",
		},
		{
			name:   "TEL international precedes fallback",
			header: `<sip:447700900111@ims.example>, <tel:+447700900222>`,
			want:   "+447700900222",
		},
		{
			name:   "first fallback gains plus",
			header: `<sip:447700900111@ims.example>, <tel:447700900222>`,
			want:   "+447700900111",
		},
		{name: "case-sensitive wire pattern", header: `<SIP:+447700900111@ims.example>`, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PickAssociatedMSISDN(test.header); got != test.want {
				t.Fatalf("PickAssociatedMSISDN = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExtractPhoneFromAssociatedMSISDN(t *testing.T) {
	tests := map[string]string{
		`display <tel:+447700900111;phone-context=ims>`: "+447700900111",
		`<SIPS:+447700900112@ims.example;user=phone>`:   "+447700900112",
		`sip:+447700900113@ims.example?header=value`:    "+447700900113",
		`tel:447700900114`:     "",
		`sip:user@ims.example`: "",
	}
	for input, want := range tests {
		if got := ExtractPhoneFromAssociatedMSISDN(input); got != want {
			t.Errorf("ExtractPhoneFromAssociatedMSISDN(%q) = %q, want %q", input, got, want)
		}
	}
}
