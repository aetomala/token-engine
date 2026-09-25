package tokenref_test

import (
	"github.com/aetomala/token-engine/internal/tokenref"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Ref", func() {
	const token = "xF7hN2kP9mQ8rT4vL6wY3gAaBbCcDdEeFfGgHhIiJjK"

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
		It("returns the fixed jwtauth-compatible vector for 'tok-xyz'", func() {
			Expect(tokenref.Ref("tok-xyz")).To(Equal("5d699dd34a86ef68"))
		})

		It("returns 16 lowercase hex characters", func() {
			Expect(tokenref.Ref(token)).To(MatchRegexp(`^[0-9a-f]{16}$`))
		})

		It("is deterministic", func() {
			Expect(tokenref.Ref(token)).To(Equal(tokenref.Ref(token)))
		})

		It("produces different outputs for different inputs", func() {
			Expect(tokenref.Ref(token)).NotTo(Equal(tokenref.Ref(token + "x")))
			Expect(tokenref.Ref("a")).NotTo(Equal(tokenref.Ref("b")))
		})

		It("does not contain the input", func() {
			Expect(tokenref.Ref(token)).NotTo(ContainSubstring(token))
		})
	})

	// ===== PHASE 6: Edge Cases =====
	Describe("Phase 6: Edge Cases", func() {
		It("returns an empty string for empty input", func() {
			Expect(tokenref.Ref("")).To(BeEmpty())
		})
	})
})
