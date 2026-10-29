package service

import "testing"

func TestDiagnosticVersionOnlyReadsTheToolBanner(t *testing.T) {
	for _, tc := range []struct{ banner, want string }{
		{"ffmpeg version n7.1.3-43-g5a1f107b4c\nbuilt with gcc 15.2.0", "7.1.3"},
		{"ffprobe version n7.1.3 Copyright", "7.1.3"},
		{"Python 3.14.9", "3.14.9"},
		{"v24.22.1", "24.22.1"},
		{"unknown banner\ncompiler 1.2.3", ""},
		{"ffmpeg version N-123-gabc\nlibavutil 59.1.2", ""},
	} {
		match := diagnosticVersion.FindStringSubmatch(tc.banner)
		got := ""
		if len(match) > 1 {
			got = match[1]
		}
		if got != tc.want {
			t.Errorf("banner %q: version=%q, want %q", tc.banner, got, tc.want)
		}
	}
}
