package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/aetomala/token-engine/internal/health"
	"github.com/aetomala/token-engine/internal/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("LiveHandler", func() {
	var (
		sut http.Handler
	)

	BeforeEach(func() {
		sut = health.NewLiveHandler()
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
	Context("GET /healthz/live", func() {
		It("returns 200 OK", func() {
			req := httptest.NewRequest("GET", "/healthz/live", nil)
			w := httptest.NewRecorder()

			sut.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})

		It("returns an empty body", func() {
			req := httptest.NewRequest("GET", "/healthz/live", nil)
			w := httptest.NewRecorder()

			sut.ServeHTTP(w, req)

			Expect(w.Body.String()).To(BeEmpty())
		})
	})
	}) // Phase 3
})

var _ = Describe("ReadyHandler", func() {
	var (
		ctrl   *gomock.Controller
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
	Context("when all checkers pass", func() {
		It("returns 200 OK", func() {
			mockChecker := testutil.NewMockChecker(ctrl)

			mockChecker.EXPECT().Check(gomock.Any()).Return(nil)

			checkers := []health.Checker{mockChecker}
			sut := health.NewReadyHandler(checkers)

			req := httptest.NewRequest("GET", "/healthz/ready", nil)
			w := httptest.NewRecorder()

			sut.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})

	Context("when one checker fails", func() {
		It("returns 503", func() {
			mockChecker := testutil.NewMockChecker(ctrl)
			mockChecker.EXPECT().Name().Return("database")
			mockChecker.EXPECT().Check(gomock.Any()).Return(errors.New("connection failed"))

			checkers := []health.Checker{mockChecker}
			sut := health.NewReadyHandler(checkers)

			req := httptest.NewRequest("GET", "/healthz/ready", nil)
			w := httptest.NewRecorder()

			sut.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
		})

		It("returns JSON body with status, failed_check name, and reason", func() {
			mockChecker := testutil.NewMockChecker(ctrl)
			mockChecker.EXPECT().Name().Return("database")
			mockChecker.EXPECT().Check(gomock.Any()).Return(errors.New("connection refused"))

			checkers := []health.Checker{mockChecker}
			sut := health.NewReadyHandler(checkers)

			req := httptest.NewRequest("GET", "/healthz/ready", nil)
			w := httptest.NewRecorder()

			sut.ServeHTTP(w, req)

			var response map[string]string
			err := json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())

			Expect(response["status"]).To(Equal("unavailable"))
			Expect(response["failed_check"]).To(Equal("database"))
			Expect(response["reason"]).To(Equal("connection refused"))
		})

		It("does not call subsequent checkers after the first failure", func() {
			mockChecker1 := testutil.NewMockChecker(ctrl)
			mockChecker2 := testutil.NewMockChecker(ctrl)

			mockChecker1.EXPECT().Name().Return("check1")
			mockChecker1.EXPECT().Check(gomock.Any()).Return(errors.New("check1 failed"))
			// Expect check2 NOT to be called
			mockChecker2.EXPECT().Check(gomock.Any()).Times(0)

			checkers := []health.Checker{mockChecker1, mockChecker2}
			sut := health.NewReadyHandler(checkers)

			req := httptest.NewRequest("GET", "/healthz/ready", nil)
			w := httptest.NewRecorder()

			sut.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
		})
	})

	Context("when checker slice is empty", func() {
		It("returns 200 OK", func() {
			checkers := []health.Checker{}
			sut := health.NewReadyHandler(checkers)

			req := httptest.NewRequest("GET", "/healthz/ready", nil)
			w := httptest.NewRecorder()

			sut.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})
	}) // Phase 3
})

var _ = Describe("AuditChecker", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		ctrl   *gomock.Controller
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
	Context("Name()", func() {
		It("returns 'audit'", func() {
			mockStore := testutil.NewMockStore(ctrl)
			checker := health.NewAuditChecker(mockStore)
			Expect(checker.Name()).To(Equal(health.CheckerNameAudit))
		})
	})

	Context("when audit.Store.Ping returns nil", func() {
		It("returns nil", func() {
			mockStore := testutil.NewMockStore(ctrl)
			mockStore.EXPECT().Ping(gomock.Any()).Return(nil)
			checker := health.NewAuditChecker(mockStore)
			err := checker.Check(ctx)
			Expect(err).To(BeNil())
		})
	})

	Context("when audit.Store.Ping returns an error", func() {
		It("returns a non-nil error", func() {
			mockStore := testutil.NewMockStore(ctrl)
			mockStore.EXPECT().Ping(gomock.Any()).Return(errors.New("store down"))
			checker := health.NewAuditChecker(mockStore)
			err := checker.Check(ctx)
			Expect(err).NotTo(BeNil())
		})
	})
	}) // Phase 3
})
