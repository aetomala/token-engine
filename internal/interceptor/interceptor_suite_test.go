package interceptor_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

func TestInterceptor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Interceptor Suite")
}
