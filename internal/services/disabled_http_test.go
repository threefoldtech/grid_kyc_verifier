package services_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/threefoldtech/tf-kyc-verifier/internal/config"
	"github.com/threefoldtech/tf-kyc-verifier/internal/handlers"
	"github.com/threefoldtech/tf-kyc-verifier/internal/models"
	"github.com/threefoldtech/tf-kyc-verifier/internal/services"
)

const (
	httpClientID  = "5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY"
	httpSponseeID = "5FHneW46xGXgs5mUapPM8ikVdJQvBLgVzW9ocmZZUadQbqh2"
)

// Pins the HTTP status codes and bodies consumers see while KYC is disabled.
// Routes mirror internal/server; the signature middleware is replaced by one
// that sets the authenticated IDs it would set.
func TestDisabledModeHTTP(t *testing.T) {
	verificationRepo := new(services.MockVerificationRepository)
	sponsorshipRepo := new(services.MockSponsorshipRepository)
	substrateClient := new(services.MockSubstrateClient)
	substrateClient.On("GetChainName").Return("Threefold Testnet", nil).Maybe()

	cfg := &config.Config{Verification: config.Verification{Disabled: true, MinBalanceToVerifyAccount: 100}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := services.NewKYCService(verificationRepo, new(services.MockTokenRepository), sponsorshipRepo,
		new(services.MockIdenfyClient), substrateClient, cfg, logger)
	require.NoError(t, err)
	h := handlers.NewHandler(service, cfg, logger)

	authed := func(c *fiber.Ctx) error {
		c.Locals("clientID", httpClientID)
		c.Locals("sponseeID", httpSponseeID)
		return c.Next()
	}
	app := fiber.New()
	app.Post("/api/v1/token", authed, h.GetOrCreateVerificationToken())
	app.Get("/api/v1/data", authed, h.GetVerificationData())
	app.Get("/api/v1/status", h.GetVerificationStatus())
	app.Post("/api/v1/sponsorships", authed, h.CreateSponsorship())

	do := func(t *testing.T, method, target string) (int, map[string]any) {
		resp, err := app.Test(httptest.NewRequest(method, target, nil))
		require.NoError(t, err)
		defer resp.Body.Close()
		var body map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		return resp.StatusCode, body
	}
	result := func(t *testing.T, body map[string]any) map[string]any {
		r, ok := body["result"].(map[string]any)
		require.True(t, ok, "response has no result object: %v", body)
		return r
	}

	t.Run("status by client_id is 200 VERIFIED", func(t *testing.T) {
		code, body := do(t, "GET", "/api/v1/status?client_id="+httpClientID)
		assert.Equal(t, fiber.StatusOK, code)
		r := result(t, body)
		assert.Equal(t, "VERIFIED", r["status"])
		assert.Equal(t, true, r["final"])
		assert.Equal(t, httpClientID, r["clientId"])
		// Grid Connect parses idenfyRef as a non-null String.
		assert.Equal(t, "", r["idenfyRef"])
	})

	t.Run("status by twin_id is 200 VERIFIED", func(t *testing.T) {
		substrateClient.On("GetAddressByTwinID", uint32(7)).Return(httpClientID, nil).Once()
		code, body := do(t, "GET", "/api/v1/status?twin_id=7")
		assert.Equal(t, fiber.StatusOK, code)
		assert.Equal(t, "VERIFIED", result(t, body)["status"])
	})

	// ZOS nodes and the Go grid client need 200 and status VERIFIED.
	t.Run("status by twin_id is 200 VERIFIED when the chain lookup fails", func(t *testing.T) {
		substrateClient.On("GetAddressByTwinID", uint32(999)).Return("", fmt.Errorf("chain unavailable")).Once()
		code, body := do(t, "GET", "/api/v1/status?twin_id=999")
		assert.Equal(t, fiber.StatusOK, code)
		assert.Equal(t, "VERIFIED", result(t, body)["status"])
	})

	t.Run("token is 409", func(t *testing.T) {
		code, body := do(t, "POST", "/api/v1/token")
		assert.Equal(t, fiber.StatusConflict, code)
		assert.Contains(t, body["error"], "user already verified")
	})

	// Unchanged from enabled mode: Grid Connect shows "No data available" on 404.
	t.Run("data with no stored record is 404", func(t *testing.T) {
		verificationRepo.On("GetVerification", mock.Anything, httpClientID).Return(nil, nil).Once()
		code, body := do(t, "GET", "/api/v1/data")
		assert.Equal(t, fiber.StatusNotFound, code)
		assert.Contains(t, body["error"], "verification not found")
	})

	t.Run("data with a stored record returns it", func(t *testing.T) {
		verificationRepo.On("GetVerification", mock.Anything, httpClientID).Return(&models.Verification{
			ClientID: httpClientID,
			Data:     models.PersonData{DocFirstName: "Ada"},
		}, nil).Once()
		code, body := do(t, "GET", "/api/v1/data")
		assert.Equal(t, fiber.StatusOK, code)
		assert.Equal(t, "Ada", result(t, body)["docFirstName"])
	})

	t.Run("sponsorship by an unverified sponsor is 401", func(t *testing.T) {
		verificationRepo.On("GetVerification", mock.Anything, httpClientID).Return(nil, nil).Once()
		code, _ := do(t, "POST", "/api/v1/sponsorships")
		assert.Equal(t, fiber.StatusUnauthorized, code)
	})

	t.Run("sponsorship by a verified sponsor is 201", func(t *testing.T) {
		overall := models.OverallApproved
		verificationRepo.On("GetVerification", mock.Anything, httpClientID).Return(&models.Verification{
			ClientID: httpClientID,
			Status:   models.Status{Overall: &overall},
		}, nil).Once()
		sponsorshipRepo.On("GetBySponsee", mock.Anything, httpSponseeID).Return(nil, nil).Once()
		sponsorshipRepo.On("Create", mock.Anything, mock.AnythingOfType("*models.Sponsorship")).Return(nil).Once()
		code, _ := do(t, "POST", "/api/v1/sponsorships")
		assert.Equal(t, fiber.StatusCreated, code)
	})

	verificationRepo.AssertExpectations(t)
	sponsorshipRepo.AssertExpectations(t)
	substrateClient.AssertExpectations(t)
}
