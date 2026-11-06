package layout

import "testing"

// Works with the real system layouts; skipped if English or Ukrainian is missing.
func TestConvertInstalled(t *testing.T) {
	ls := Installed()
	for _, l := range ls {
		t.Logf("%s hkl=%#x keys=%d", l.Name, l.HKL, len(l.char))
	}
	var en, uk *Layout
	for _, l := range ls {
		switch l.Name {
		case "EN":
			en = l
		case "UK":
			uk = l
		}
	}
	if en == nil || uk == nil {
		t.Skip("English and Ukrainian layouts are required")
	}

	for _, tt := range []struct {
		in, want string
		from, to *Layout
	}{
		{"ghbdsn", "привіт", en, uk},
		{"Ghbdsn cdsn", "Привіт світ", en, uk},
		{"'rcgjhn", "єкспорт", en, uk},
		{"руддщ", "hello", uk, en},
		{"Ш фь", "I am", uk, en},
		{"ghbdsn, cdsn! Ïë", "привітб світ! Ïë", en, uk}, // "," is the "б" key; characters without a key are kept
	} {
		if got := Convert(tt.in, tt.from, tt.to); got != tt.want {
			t.Errorf("Convert(%q, %s→%s) = %q, want %q", tt.in, tt.from.Name, tt.to.Name, got, tt.want)
		}
	}

	idx := func(l *Layout) int {
		for i := range ls {
			if ls[i] == l {
				return i
			}
		}
		return -1
	}
	if got := Detect(ls, "ghbdsn.", 0); got != idx(en) {
		t.Errorf("Detect(ghbdsn.) = %d, want EN (%d)", got, idx(en))
	}
	if got := Detect(ls, "руддщ", 0); got != idx(uk) {
		t.Errorf("Detect(руддщ) = %d, want UK (%d)", got, idx(uk))
	}
}
