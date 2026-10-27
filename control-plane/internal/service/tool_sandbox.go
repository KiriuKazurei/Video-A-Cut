package service

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

// This is an application tool boundary, not an OS security sandbox. Only
// reviewed executables receive validated paths; models never supply commands.
func toolEnvironment(tokenNames ...string) []string {
	allowed := map[string]bool{}
	for _, name := range []string{"PATH", "SYSTEMROOT", "SYSTEMDRIVE", "PROGRAMDATA", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP", "TMPDIR", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "VAC_TEST_ROOT", "VAC_TEST_OUTPUT", "VAC_DRAW_BROWSER", "PLAYWRIGHT_BROWSERS_PATH", "LOCALAPPDATA"} {
		allowed[name] = true
	}
	for _, name := range tokenNames {
		if name != "" && strings.ToUpper(name) != "VAC_HUMAN_ACCESS_TOKEN" {
			allowed[strings.ToUpper(name)] = true
		}
	}
	out := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if allowed[strings.ToUpper(name)] {
			out = append(out, entry)
		}
	}
	out = append(out, "PYTHONIOENCODING=utf-8", "PYTHONDONTWRITEBYTECODE=1", "PYTHONNOUSERSITE=1")
	sort.Strings(out)
	return out
}
func payloadCredentialNames(payload any, sections ...string) []string {
	// Only Go-created Provider structures can reach this function, after scope
	// validation. Caller-supplied tool arguments never select credential names.
	raw, _ := json.Marshal(payload)
	var root map[string]any
	_ = json.Unmarshal(raw, &root)
	names := []string{}
	var visit func(any)
	visit = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, item := range x {
				if k == "token_env" {
					if name, ok := item.(string); ok {
						names = append(names, name)
					}
				} else {
					visit(item)
				}
			}
		case []any:
			for _, item := range x {
				visit(item)
			}
		}
	}
	for _, section := range sections {
		var selected any = root
		for _, part := range strings.Split(section, ".") {
			m, ok := selected.(map[string]any)
			if !ok {
				selected = nil
				break
			}
			selected = m[part]
		}
		visit(selected)
	}
	return names
}
