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

func ParseGitAction(s string) dotfilesdv1.GitAction {
	key := "GIT_ACTION_" + strings.ToUpper(s)
	if v, ok := dotfilesdv1.GitAction_value[key]; ok {
		return dotfilesdv1.GitAction(v)
	}
	return dotfilesdv1.GitAction_GIT_ACTION_UNSPECIFIED
}

func ParseReloadTarget(s string) dotfilesdv1.ReloadTarget {
	key := "RELOAD_TARGET_" + strings.ToUpper(s)
	if v, ok := dotfilesdv1.ReloadTarget_value[key]; ok {
		return dotfilesdv1.ReloadTarget(v)
	}
	return dotfilesdv1.ReloadTarget_RELOAD_TARGET_UNSPECIFIED
}

func ParseSudoMethod(s string) dotfilesdv1.SudoMethod {
	key := "SUDO_METHOD_" + strings.ToUpper(s)
	if v, ok := dotfilesdv1.SudoMethod_value[key]; ok {
		return dotfilesdv1.SudoMethod(v)
	}
	return dotfilesdv1.SudoMethod_SUDO_METHOD_UNSPECIFIED
}
