package rail

import "testing"

func TestVersionPrecedenceAndRanges(t *testing.T) {
	ordered := []string{
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
	}
	for index := 0; index < len(ordered)-1; index++ {
		left, err := ParseVersion(ordered[index])
		if err != nil {
			t.Fatal(err)
		}
		right, err := ParseVersion(ordered[index+1])
		if err != nil {
			t.Fatal(err)
		}
		if left.Compare(right) >= 0 {
			t.Fatalf("expected %s before %s", left, right)
		}
	}
	cases := []struct {
		rangeText string
		version   string
		want      bool
	}{
		{"^1.2.3", "1.9.0", true},
		{"^1.2.3", "2.0.0", false},
		{"~1.2.3", "1.2.9", true},
		{"~1.2.3", "1.3.0", false},
		{"1.x", "1.99.4", true},
		{"1.x", "2.0.0", false},
		{">=1.0.0 <2.0.0", "1.5.0", true},
		{"1.0.0 - 1.2.0", "1.2.0", true},
		{"<1.0.0 || >=2.0.0", "2.1.0", true},
	}
	for _, test := range cases {
		rangeValue, err := ParseRange(test.rangeText)
		if err != nil {
			t.Fatalf("ParseRange(%q): %v", test.rangeText, err)
		}
		version, err := ParseVersion(test.version)
		if err != nil {
			t.Fatal(err)
		}
		if got := rangeValue.Contains(version); got != test.want {
			t.Errorf("range %q contains %s = %t, want %t", test.rangeText, test.version, got, test.want)
		}
	}
}

func TestVersionRejectsMalformedInput(t *testing.T) {
	for _, input := range []string{"", "1", "1.2", "01.2.3", "1.2.3-01", "1.2.3+", "1.2.a"} {
		if _, err := ParseVersion(input); err == nil {
			t.Errorf("ParseVersion(%q) unexpectedly succeeded", input)
		}
	}
}
