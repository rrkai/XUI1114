package cronspec

import "testing"

func TestParse(t *testing.T) {
	for _, spec := range []string{"*/30 * * * *", "30 * * * * *", "@daily", "@every 8h"} {
		schedule, err := Parse(spec)
		if err != nil || schedule == nil {
			t.Fatalf("Parse(%q) = %v, %v; want a schedule", spec, schedule, err)
		}
	}

	for _, spec := range []string{"", "  ", "off", "OFF"} {
		schedule, err := Parse(spec)
		if err != nil || schedule != nil {
			t.Fatalf("Parse(%q) = %v, %v; want nil, nil", spec, schedule, err)
		}
	}

	for _, spec := range []string{"not a schedule", "61 * * * *", "* * *"} {
		if _, err := Parse(spec); err == nil {
			t.Fatalf("Parse(%q) succeeded; want an error", spec)
		}
	}
}
