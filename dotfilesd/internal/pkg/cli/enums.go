package cli

import (
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

func ParseLogLevel(s string) dotfilesdv1.LogLevel {
	key := "LOG_LEVEL_" + strings.ToUpper(s)
	if v, ok := dotfilesdv1.LogLevel_value[key]; ok {
		return dotfilesdv1.LogLevel(v)
	}
	if strings.ToLower(s) == "warn" {
		return dotfilesdv1.LogLevel_LOG_LEVEL_WARN
	}
	return dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED
}

func ParseSudoMethod(s string) dotfilesdv1.SudoMethod {
	key := "SUDO_METHOD_" + strings.ToUpper(s)
	if v, ok := dotfilesdv1.SudoMethod_value[key]; ok {
		return dotfilesdv1.SudoMethod(v)
	}
	return dotfilesdv1.SudoMethod_SUDO_METHOD_UNSPECIFIED
}
