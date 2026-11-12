package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/threefoldtech/tf-kyc-verifier/internal/errors"
	"github.com/threefoldtech/tf-kyc-verifier/internal/models"
)

const (
	testClientID  = "5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY"
	testSponseeID = "5FHneW46xGXgs5mUapPM8ikVdJQvBLgVzW9ocmZZUadQbqh2"
)

// In these tests a mock without an expectation panics when called, so a test
// that sets none proves the call never reached the database, the chain or the
// verification provider.
func TestDisabledMode(t *testing.T) {
	ctx := context.Background()

	setup := func(t *testing.T) (*KYCService, *MockVerificationRepository, *MockSponsorshipRepository, *MockSubstrateClient) {
		service, verificationRepo, _, sponsorshipRepo, _, substrateClient := setupKYCService(t)
		service.config.Disabled = true
		return service, verificationRepo, sponsorshipRepo, substrateClient
	}

	t.Run("status reports every client as verified without a lookup", func(t *testing.T) {
		service, _, _, _ := setup(t)

		outcome, err := service.GetVerificationStatus(ctx, testClientID)

		require.NoError(t, err)
		require.NotNil(t, outcome)
		assert.Equal(t, models.OutcomeApproved, outcome.Outcome)
		assert.Equal(t, testClientID, outcome.ClientID)
		require.NotNil(t, outcome.Final)
		assert.True(t, *outcome.Final)
	})

	t.Run("status by twin ID resolves the address, then reports verified", func(t *testing.T) {
		service, _, _, substrateClient := setup(t)
		substrateClient.On("GetAddressByTwinID", uint32(7)).Return(testClientID, nil).Once()

		outcome, err := service.GetVerificationStatusByTwinID(ctx, 7)

		require.NoError(t, err)
		require.NotNil(t, outcome)
		assert.Equal(t, models.OutcomeApproved, outcome.Outcome)
		assert.Equal(t, testClientID, outcome.ClientID)
		substrateClient.AssertExpectations(t)
	})

	t.Run("status by twin ID still fails when the chain lookup fails", func(t *testing.T) {
		service, _, _, substrateClient := setup(t)
		substrateClient.On("GetAddressByTwinID", uint32(999)).Return("", fmt.Errorf("twin not found")).Once()

		outcome, err := service.GetVerificationStatusByTwinID(ctx, 999)

		assert.Nil(t, outcome)
		var serviceErr *errors.ServiceError
		require.ErrorAs(t, err, &serviceErr)
		assert.Equal(t, errors.ErrorTypeExternal, serviceErr.Type)
	})

	t.Run("disabled overrides always-verified-only mode", func(t *testing.T) {
		service, _, _, _ := setup(t)
		service.config.AlwaysVerifiedIDs = []string{"someone-else"}
		service.config.AlwaysVerifiedIDsOnly = true

		outcome, err := service.GetVerificationStatus(ctx, testClientID)

		require.NoError(t, err)
		require.NotNil(t, outcome)
		assert.Equal(t, models.OutcomeApproved, outcome.Outcome)
	})

	t.Run("every user counts as verified", func(t *testing.T) {
		service, _, _, _ := setup(t)

		verified, err := service.IsUserVerified(ctx, testClientID)

		require.NoError(t, err)
		assert.True(t, verified)
	})

	t.Run("token request is refused without starting a session", func(t *testing.T) {
		service, _, _, _ := setup(t)

		token, isNew, err := service.GetOrCreateVerificationToken(ctx, testClientID)

		assert.Nil(t, token)
		assert.False(t, isNew)
		var serviceErr *errors.ServiceError
		require.ErrorAs(t, err, &serviceErr)
		assert.Equal(t, errors.ErrorTypeConflict, serviceErr.Type)
	})

	t.Run("data returns the stored record when there is one", func(t *testing.T) {
		service, verificationRepo, _, _ := setup(t)
		stored := &models.Verification{ClientID: testClientID, Data: models.PersonData{DocFirstName: "Ada"}}
		verificationRepo.On("GetVerification", ctx, testClientID).Return(stored, nil).Once()

		verification, err := service.GetVerificationData(ctx, testClientID)

		require.NoError(t, err)
		assert.Same(t, stored, verification)
		verificationRepo.AssertExpectations(t)
	})

	t.Run("data returns nothing when there is no stored record", func(t *testing.T) {
		service, verificationRepo, _, _ := setup(t)
		verificationRepo.On("GetVerification", ctx, testClientID).Return(nil, nil).Once()

		verification, err := service.GetVerificationData(ctx, testClientID)

		require.NoError(t, err)
		assert.Nil(t, verification)
		verificationRepo.AssertExpectations(t)
	})

	// Being reported as verified does not make someone a sponsor: the sponsor
	// check still needs a stored approved verification.
	t.Run("sponsor without a stored verification is refused", func(t *testing.T) {
		service, verificationRepo, _, _ := setup(t)
		verificationRepo.On("GetVerification", ctx, testClientID).Return(nil, nil).Once()

		sponsorship, err := service.CreateSponsorship(ctx, testClientID, testSponseeID)

		assert.Nil(t, sponsorship)
		var serviceErr *errors.ServiceError
		require.ErrorAs(t, err, &serviceErr)
		assert.Equal(t, errors.ErrorTypeAuthorization, serviceErr.Type)
		verificationRepo.AssertExpectations(t)
	})

	t.Run("verified sponsor can still sponsor", func(t *testing.T) {
		service, verificationRepo, sponsorshipRepo, _ := setup(t)
		overall := models.OverallApproved
		verificationRepo.On("GetVerification", ctx, testClientID).Return(&models.Verification{
			ClientID: testClientID,
			Status:   models.Status{Overall: &overall},
		}, nil).Once()
		sponsorshipRepo.On("GetBySponsee", ctx, testSponseeID).Return(nil, nil).Once()
		sponsorshipRepo.On("Create", ctx, mock.AnythingOfType("*models.Sponsorship")).Return(nil).Once()

		sponsorship, err := service.CreateSponsorship(ctx, testClientID, testSponseeID)

		require.NoError(t, err)
		require.NotNil(t, sponsorship)
		assert.Equal(t, testClientID, sponsorship.SponsorClientID)
		verificationRepo.AssertExpectations(t)
		sponsorshipRepo.AssertExpectations(t)
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

	t.Run("client with no record and no sponsor is not verified", func(t *testing.T) {
		service, verificationRepo, _, sponsorshipRepo, _, _ := setupKYCService(t)
		verificationRepo.On("GetVerification", ctx, testClientID).Return(nil, nil).Times(3)
		sponsorshipRepo.On("GetBySponsee", ctx, testClientID).Return(nil, nil).Twice()

		outcome, err := service.GetVerificationStatus(ctx, testClientID)
		require.NoError(t, err)
		assert.Nil(t, outcome)

		verified, err := service.IsUserVerified(ctx, testClientID)
		require.NoError(t, err)
		assert.False(t, verified)

		data, err := service.GetVerificationData(ctx, testClientID)
		require.NoError(t, err)
		assert.Nil(t, data)

		verificationRepo.AssertExpectations(t)
		sponsorshipRepo.AssertExpectations(t)
	})
}
