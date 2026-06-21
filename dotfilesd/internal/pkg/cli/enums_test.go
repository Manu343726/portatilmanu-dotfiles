package cli

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

var _ = Describe("ParseLogLevel", func() {
	It("parses 'trace'", func() {
		Expect(ParseLogLevel("trace")).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_TRACE))
	})

	It("parses 'debug'", func() {
		Expect(ParseLogLevel("debug")).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_DEBUG))
	})

	It("parses 'info'", func() {
		Expect(ParseLogLevel("info")).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_INFO))
	})

	It("parses 'warn'", func() {
		Expect(ParseLogLevel("warn")).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_WARN))
	})

	It("parses 'error'", func() {
		Expect(ParseLogLevel("error")).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_ERROR))
	})

	It("is case-insensitive", func() {
		Expect(ParseLogLevel("DEBUG")).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_DEBUG))
		Expect(ParseLogLevel("Info")).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_INFO))
	})

	It("returns UNSPECIFIED for unknown level", func() {
		Expect(ParseLogLevel("unknown")).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED))
	})
})

var _ = Describe("ParseGitAction", func() {
	It("parses 'status'", func() {
		Expect(ParseGitAction("status")).To(Equal(dotfilesdv1.GitAction_GIT_ACTION_STATUS))
	})

	It("parses 'diff'", func() {
		Expect(ParseGitAction("diff")).To(Equal(dotfilesdv1.GitAction_GIT_ACTION_DIFF))
	})

	It("parses 'add'", func() {
		Expect(ParseGitAction("add")).To(Equal(dotfilesdv1.GitAction_GIT_ACTION_ADD))
	})

	It("parses 'commit'", func() {
		Expect(ParseGitAction("commit")).To(Equal(dotfilesdv1.GitAction_GIT_ACTION_COMMIT))
	})

	It("parses 'push'", func() {
		Expect(ParseGitAction("push")).To(Equal(dotfilesdv1.GitAction_GIT_ACTION_PUSH))
	})

	It("parses 'log'", func() {
		Expect(ParseGitAction("log")).To(Equal(dotfilesdv1.GitAction_GIT_ACTION_LOG))
	})

	It("returns UNSPECIFIED for unknown action", func() {
		Expect(ParseGitAction("unknown")).To(Equal(dotfilesdv1.GitAction_GIT_ACTION_UNSPECIFIED))
	})
})

var _ = Describe("ParseReloadTarget", func() {
	It("parses 'tmux'", func() {
		Expect(ParseReloadTarget("tmux")).To(Equal(dotfilesdv1.ReloadTarget_RELOAD_TARGET_TMUX))
	})

	It("parses 'i3'", func() {
		Expect(ParseReloadTarget("i3")).To(Equal(dotfilesdv1.ReloadTarget_RELOAD_TARGET_I3))
	})

	It("parses 'kitty'", func() {
		Expect(ParseReloadTarget("kitty")).To(Equal(dotfilesdv1.ReloadTarget_RELOAD_TARGET_KITTY))
	})

	It("parses 'all'", func() {
		Expect(ParseReloadTarget("all")).To(Equal(dotfilesdv1.ReloadTarget_RELOAD_TARGET_ALL))
	})

	It("returns UNSPECIFIED for unknown target", func() {
		Expect(ParseReloadTarget("unknown")).To(Equal(dotfilesdv1.ReloadTarget_RELOAD_TARGET_UNSPECIFIED))
	})
})

var _ = Describe("ParseSudoMethod", func() {
	It("parses 'nopass'", func() {
		Expect(ParseSudoMethod("nopass")).To(Equal(dotfilesdv1.SudoMethod_SUDO_METHOD_NOPASS))
	})

	It("parses 'graphical'", func() {
		Expect(ParseSudoMethod("graphical")).To(Equal(dotfilesdv1.SudoMethod_SUDO_METHOD_GRAPHICAL))
	})

	It("returns UNSPECIFIED for unknown method", func() {
		Expect(ParseSudoMethod("unknown")).To(Equal(dotfilesdv1.SudoMethod_SUDO_METHOD_UNSPECIFIED))
	})
})
