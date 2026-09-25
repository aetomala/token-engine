package tokenref_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTokenRef(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TokenRef Suite")
}
