package daemon

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parseScript", func() {
	It("parses a simple exec command", func() {
		steps, err := parseScript("echo hello")
		Expect(err).To(Succeed())
		Expect(steps).To(HaveLen(1))
		Expect(steps[0].kind).To(Equal("exec"))
		Expect(steps[0].command).To(Equal("echo hello"))
	})

	It("parses multiple exec commands", func() {
		steps, err := parseScript("echo first\necho second\necho third")
		Expect(err).To(Succeed())
		Expect(steps).To(HaveLen(3))
		for _, s := range steps {
			Expect(s.kind).To(Equal("exec"))
		}
	})

	It("skips empty lines and comments", func() {
		steps, err := parseScript("# comment\n\necho hello\n# another\n\necho world")
		Expect(err).To(Succeed())
		Expect(steps).To(HaveLen(2))
		Expect(steps[0].command).To(Equal("echo hello"))
		Expect(steps[1].command).To(Equal("echo world"))
	})

	It("parses @confirm directive", func() {
		steps, err := parseScript(`@confirm "Are you sure?"`)
		Expect(err).To(Succeed())
		Expect(steps).To(HaveLen(1))
		Expect(steps[0].kind).To(Equal("confirm"))
		Expect(steps[0].message).To(Equal("Are you sure?"))
		Expect(steps[0].varName).To(Equal("_confirm"))
	})

	It("parses @input directive with default var", func() {
		steps, err := parseScript(`@input "Enter value:"`)
		Expect(err).To(Succeed())
		Expect(steps).To(HaveLen(1))
		Expect(steps[0].kind).To(Equal("input"))
		Expect(steps[0].message).To(Equal("Enter value:"))
		Expect(steps[0].varName).To(Equal("_input"))
	})

	It("parses @input directive with custom var", func() {
		steps, err := parseScript(`@input "Enter name:" as NAME`)
		Expect(err).To(Succeed())
		Expect(steps).To(HaveLen(1))
		Expect(steps[0].kind).To(Equal("input"))
		Expect(steps[0].message).To(Equal("Enter name:"))
		Expect(steps[0].varName).To(Equal("NAME"))
	})

	It("parses @choose directive with default var", func() {
		steps, err := parseScript(`@choose "Select:" "opt1" "opt2"`)
		Expect(err).To(Succeed())
		Expect(steps).To(HaveLen(1))
		Expect(steps[0].kind).To(Equal("choose"))
		Expect(steps[0].message).To(Equal("Select:"))
		Expect(steps[0].options).To(Equal([]string{"opt1", "opt2"}))
		Expect(steps[0].varName).To(Equal("_choose"))
	})

	It("parses @choose directive with custom var", func() {
		steps, err := parseScript(`@choose "Pick:" "a" "b" "c" as SELECTION`)
		Expect(err).To(Succeed())
		Expect(steps).To(HaveLen(1))
		Expect(steps[0].kind).To(Equal("choose"))
		Expect(steps[0].options).To(Equal([]string{"a", "b", "c"}))
		Expect(steps[0].varName).To(Equal("SELECTION"))
	})

	It("parses mixed content", func() {
		content := `echo "starting"
@confirm "Continue?"
echo "done"`
		steps, err := parseScript(content)
		Expect(err).To(Succeed())
		Expect(steps).To(HaveLen(3))
		Expect(steps[0].kind).To(Equal("exec"))
		Expect(steps[1].kind).To(Equal("confirm"))
		Expect(steps[2].kind).To(Equal("exec"))
	})

	It("returns error for unknown directive", func() {
		_, err := parseScript(`@unknown "something"`)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unknown directive"))
	})

	It("returns error for malformed @confirm", func() {
		_, err := parseScript(`@confirm`)
		Expect(err).To(HaveOccurred())
	})

	It("returns error for @choose with no options", func() {
		_, err := parseScript(`@choose "only prompt"`)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("extractQuotedString", func() {
	It("extracts a simple quoted string", func() {
		s, err := extractQuotedString(`"hello"`)
		Expect(err).To(Succeed())
		Expect(s).To(Equal("hello"))
	})

	It("handles escaped quotes", func() {
		s, err := extractQuotedString(`"hello \"world\""`)
		Expect(err).To(Succeed())
		Expect(s).To(Equal(`hello "world"`))
	})

	It("returns error for unclosed quote", func() {
		_, err := extractQuotedString(`"unclosed`)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("extractFirstQuoted", func() {
	It("extracts first quoted string and returns remainder", func() {
		val, rest, err := extractFirstQuoted(`"first" "second"`)
		Expect(err).To(Succeed())
		Expect(val).To(Equal("first"))
		Expect(rest).To(Equal(`"second"`))
	})
})

var _ = Describe("parseChooseArgs", func() {
	It("parses prompt with two options", func() {
		prompt, options, varName, err := parseChooseArgs(`"Select" "A" "B"`)
		Expect(err).To(Succeed())
		Expect(prompt).To(Equal("Select"))
		Expect(options).To(Equal([]string{"A", "B"}))
		Expect(varName).To(Equal("_choose"))
	})

	It("parses prompt with options and custom var", func() {
		prompt, options, varName, err := parseChooseArgs(`"Pick" "x" "y" as RESULT`)
		Expect(err).To(Succeed())
		Expect(prompt).To(Equal("Pick"))
		Expect(options).To(Equal([]string{"x", "y"}))
		Expect(varName).To(Equal("RESULT"))
	})

	It("returns error for missing prompt", func() {
		_, _, _, err := parseChooseArgs(``)
		Expect(err).To(HaveOccurred())
	})
})
