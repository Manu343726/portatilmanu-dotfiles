package daemon

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Helpers", func() {
	Describe("truncate", func() {
		It("returns the string unchanged when within limit", func() {
			Expect(truncate("hello", 10)).To(Equal("hello"))
		})

		It("truncates and appends ... when over limit", func() {
			Expect(truncate("hello world", 5)).To(Equal("hello..."))
		})

		It("handles empty string", func() {
			Expect(truncate("", 5)).To(Equal(""))
		})

		It("handles exact boundary", func() {
			Expect(truncate("hello", 5)).To(Equal("hello"))
		})

		It("handles zero limit", func() {
			Expect(truncate("hello", 0)).To(Equal("..."))
		})

		It("handles very long strings", func() {
			long := "abcdefghijklmnopqrstuvwxyz"
			Expect(truncate(long, 10)).To(Equal("abcdefghij..."))
		})
	})

	Describe("zeroBytes", func() {
		It("zeroes out a byte slice", func() {
			b := []byte("hello")
			zeroBytes(b)
			for _, v := range b {
				Expect(v).To(BeZero())
			}
		})

		It("handles empty slice", func() {
			b := []byte{}
			zeroBytes(b) // should not panic
			Expect(b).To(BeEmpty())
		})

		It("handles nil slice", func() {
			var b []byte
			zeroBytes(b) // should not panic
			Expect(b).To(BeNil())
		})

		It("zeroes a long slice correctly", func() {
			b := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
			zeroBytes(b)
			for _, v := range b {
				Expect(v).To(BeZero())
			}
		})
	})

	Describe("fmtSscanf", func() {
		It("parses integers correctly", func() {
			var v int
			n, err := fmtSscanf("42", &v)
			Expect(err).To(Succeed())
			Expect(n).To(BeNumerically(">", 0))
			Expect(v).To(Equal(42))
		})

		It("parses zero", func() {
			var v int
			_, err := fmtSscanf("0", &v)
			Expect(err).To(Succeed())
			Expect(v).To(Equal(0))
		})

		It("parses negative numbers", func() {
			var v int
			_, err := fmtSscanf("-7", &v)
			Expect(err).To(Succeed())
			Expect(v).To(Equal(-7))
		})
	})

	Describe("runCmd", func() {
		It("executes a command and returns trimmed output", func() {
			out, err := runCmd("echo", "hello")
			Expect(err).To(Succeed())
			Expect(out).To(Equal("hello"))
		})

		It("returns error for non-existent command", func() {
			_, err := runCmd("nonexistent_cmd_xyz")
			Expect(err).To(HaveOccurred())
		})

		It("trims whitespace from output", func() {
			out, err := runCmd("printf", "  spaced  ")
			Expect(err).To(Succeed())
			Expect(out).To(Equal("spaced"))
		})
	})

	Describe("runCmdFull", func() {
		It("captures stdout", func() {
			stdout, stderr, code := runCmdFull("echo", "hello")
			Expect(code).To(Equal(0))
			Expect(stdout).To(Equal("hello\n"))
			Expect(stderr).To(BeEmpty())
		})

		It("captures stderr", func() {
			stdout, stderr, code := runCmdFull("sh", "-c", "echo err >&2")
			Expect(code).To(Equal(0))
			Expect(stdout).To(BeEmpty())
			Expect(stderr).To(Equal("err\n"))
		})

		It("returns non-zero exit code on failure", func() {
			_, _, code := runCmdFull("sh", "-c", "exit 42")
			Expect(code).To(Equal(42))
		})

		It("returns -1 for command that cannot start", func() {
			_, _, code := runCmdFull("nonexistent_cmd_xyz")
			Expect(code).To(Equal(-1))
		})
	})

	Describe("hasSudo and hasPkexec", func() {
		It("detects sudo availability", func() {
			// sudo should be available on this system
			Expect(hasSudo()).To(BeTrue())
		})

		It("detects pkexec availability", func() {
			// We just check it doesn't panic
			_ = hasPkexec()
		})
	})
})
