package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/threefoldtech/tf-kyc-verifier/internal/errors"
	"github.com/threefoldtech/tf-kyc-verifier/internal/models"
)

const testClientID = "5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY"

// With VERIFICATION_DISABLED set, no call may reach the database, the chain
// or the verification provider: the mocks carry no expectations, so any call
// to them fails the test.
func TestDisabledMode(t *testing.T) {
	ctx := context.Background()

	setup := func(t *testing.T) (*KYCService, *MockVerificationRepository, *MockTokenRepository, *MockSponsorshipRepository, *MockIdenfyClient) {
		service, verificationRepo, tokenRepo, sponsorshipRepo, idenfyClient, _ := setupKYCService(t)
		service.config.Disabled = true
		return service, verificationRepo, tokenRepo, sponsorshipRepo, idenfyClient
	}

	t.Run("status reports every client as verified", func(t *testing.T) {
		service, verificationRepo, _, sponsorshipRepo, _ := setup(t)

		outcome, err := service.GetVerificationStatus(ctx, testClientID)

		require.NoError(t, err)
		require.NotNil(t, outcome)
		assert.Equal(t, models.OutcomeApproved, outcome.Outcome)
		assert.Equal(t, testClientID, outcome.ClientID)
		require.NotNil(t, outcome.Final)
		assert.True(t, *outcome.Final)
		verificationRepo.AssertNotCalled(t, "GetVerification", mock.Anything, mock.Anything)
		sponsorshipRepo.AssertNotCalled(t, "GetBySponsee", mock.Anything, mock.Anything)
	})

	t.Run("disabled overrides always-verified-only mode", func(t *testing.T) {
		service, _, _, _, _ := setup(t)
		service.config.AlwaysVerifiedIDs = []string{"someone-else"}
		service.config.AlwaysVerifiedIDsOnly = true

		outcome, err := service.GetVerificationStatus(ctx, testClientID)

		require.NoError(t, err)
		require.NotNil(t, outcome)
		assert.Equal(t, models.OutcomeApproved, outcome.Outcome)
	})

	t.Run("every user counts as verified", func(t *testing.T) {
		service, verificationRepo, _, _, _ := setup(t)

		verified, err := service.IsUserVerified(ctx, testClientID)

		require.NoError(t, err)
		assert.True(t, verified)
		verificationRepo.AssertNotCalled(t, "GetVerification", mock.Anything, mock.Anything)
	})

	t.Run("token request is refused without starting a session", func(t *testing.T) {
		service, _, tokenRepo, _, idenfyClient := setup(t)

		token, isNew, err := service.GetOrCreateVerificationToken(ctx, testClientID)

		assert.Nil(t, token)
		assert.False(t, isNew)
		var serviceErr *errors.ServiceError
		require.ErrorAs(t, err, &serviceErr)
		assert.Equal(t, errors.ErrorTypeConflict, serviceErr.Type)
		tokenRepo.AssertNotCalled(t, "GetToken", mock.Anything, mock.Anything)
		idenfyClient.AssertNotCalled(t, "CreateVerificationSession", mock.Anything, mock.Anything)
	})

	t.Run("data endpoint returns an empty record, not stored data", func(t *testing.T) {
		service, verificationRepo, _, _, _ := setup(t)

		verification, err := service.GetVerificationData(ctx, testClientID)

		require.NoError(t, err)
		require.NotNil(t, verification)
		assert.Equal(t, testClientID, verification.ClientID)
		assert.Equal(t, models.PersonData{DuplicateFaces: []string{}, DuplicateDocFaces: []string{}}, verification.Data)
		verificationRepo.AssertNotCalled(t, "GetVerification", mock.Anything, mock.Anything)
	})

	t.Run("new sponsorships are refused and nothing is stored", func(t *testing.T) {
		service, verificationRepo, _, sponsorshipRepo, _ := setup(t)

		sponsorship, err := service.CreateSponsorship(ctx, testClientID, "5FHneW46xGXgs5mUapPM8ikVdJQvBLgVzW9ocmZZUadQbqh2")

		assert.Nil(t, sponsorship)
		var serviceErr *errors.ServiceError
		require.ErrorAs(t, err, &serviceErr)
		assert.Equal(t, errors.ErrorTypeForbidden, serviceErr.Type)
		verificationRepo.AssertNotCalled(t, "GetVerification", mock.Anything, mock.Anything)
		sponsorshipRepo.AssertNotCalled(t, "GetBySponsee", mock.Anything, mock.Anything)
		sponsorshipRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	})
}

// With the flag off, status still comes from the stored record.
func TestEnabledModeReadsStoredVerification(t *testing.T) {
	ctx := context.Background()

	t.Run("stored approved record is reported as approved", func(t *testing.T) {
		service, verificationRepo, _, _, _, _ := setupKYCService(t)
		final := true
		overall := models.OverallApproved
		verificationRepo.On("GetVerification", ctx, testClientID).Return(&models.Verification{
			Final:    &final,
			ClientID: testClientID,
			Status:   models.Status{Overall: &overall},
		}, nil).Once()

		outcome, err := service.GetVerificationStatus(ctx, testClientID)

		require.NoError(t, err)
		require.NotNil(t, outcome)
		assert.Equal(t, models.OutcomeApproved, outcome.Outcome)
		verificationRepo.AssertExpectations(t)
	})

	t.Run("client with no record and no sponsor is not found", func(t *testing.T) {
		service, verificationRepo, _, sponsorshipRepo, _, _ := setupKYCService(t)
		verificationRepo.On("GetVerification", ctx, testClientID).Return(nil, nil).Twice()
		sponsorshipRepo.On("GetBySponsee", ctx, testClientID).Return(nil, nil).Twice()

		outcome, err := service.GetVerificationStatus(ctx, testClientID)
		require.NoError(t, err)
		assert.Nil(t, outcome)

		verified, err := service.IsUserVerified(ctx, testClientID)
		require.NoError(t, err)
		assert.False(t, verified)
		verificationRepo.AssertExpectations(t)
		sponsorshipRepo.AssertExpectations(t)
	})
}
