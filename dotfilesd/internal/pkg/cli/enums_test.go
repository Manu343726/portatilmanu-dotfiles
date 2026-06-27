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
