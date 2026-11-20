package collector

import (
	"os"
	"strconv"
	"strings"
)

func OSName() string {
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		if data, err := os.ReadFile(path); err == nil {
			return parseOSRelease(string(data))
		}
	}
	return ""
}

func parseOSRelease(data string) string {
	values := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || (key != "PRETTY_NAME" && key != "NAME" && key != "VERSION") {
			continue
		}
		value = strings.TrimSpace(value)
		if decoded, err := strconv.Unquote(value); err == nil {
			value = decoded
		} else if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' ||
			value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	if value := values["PRETTY_NAME"]; value != "" {
		return clean(value)
	}
	return clean(values["NAME"] + " " + values["VERSION"])
}
