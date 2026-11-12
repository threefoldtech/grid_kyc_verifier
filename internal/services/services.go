/*
Package services contains the services for the application.
This layer is responsible for handling the business logic.
*/
package services

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/threefoldtech/tf-kyc-verifier/internal/clients/idenfy"
	"github.com/threefoldtech/tf-kyc-verifier/internal/clients/substrate"
	"github.com/threefoldtech/tf-kyc-verifier/internal/config"
	"github.com/threefoldtech/tf-kyc-verifier/internal/errors"
	"github.com/threefoldtech/tf-kyc-verifier/internal/models"
	"github.com/threefoldtech/tf-kyc-verifier/internal/repository"
)

const TFT_CONVERSION_FACTOR = 10000000

type KYCService struct {
	verificationRepo repository.VerificationRepository
	tokenRepo        repository.TokenRepository
	sponsorshipRepo  repository.SponsorshipRepository
	idenfy           idenfy.IdenfyClient
	substrate        substrate.SubstrateClient
	config           *config.Verification
	logger           *slog.Logger
	IdenfySuffix     string
}

func NewKYCService(verificationRepo repository.VerificationRepository, tokenRepo repository.TokenRepository, sponsorshipRepo repository.SponsorshipRepository, idenfy idenfy.IdenfyClient, substrateClient substrate.SubstrateClient, config *config.Config, logger *slog.Logger) (*KYCService, error) {
	idenfySuffix, err := GetIdenfySuffix(substrateClient, config)
	if err != nil {
		return nil, fmt.Errorf("getting idenfy suffix: %w", err)
	}
	if config.Verification.Disabled {
		logger.Warn("KYC is disabled (VERIFICATION_DISABLED=true): all clients are reported as verified, no verification sessions are started, and new sponsorships are refused")
	}
	return &KYCService{
		verificationRepo: verificationRepo,
		tokenRepo:        tokenRepo,
		sponsorshipRepo:  sponsorshipRepo,
		idenfy:           idenfy,
		substrate:        substrateClient,
		config:           &config.Verification,
		logger:           logger,
		IdenfySuffix:     idenfySuffix,
	}, nil
}

func GetIdenfySuffix(substrateClient substrate.SubstrateClient, config *config.Config) (string, error) {
	idenfySuffix, err := GetChainNetworkName(substrateClient)
	if err != nil {
		return "", fmt.Errorf("getting chain network name: %w", err)
	}
	if config.Idenfy.Namespace != "" {
		idenfySuffix = config.Idenfy.Namespace + ":" + idenfySuffix
	}
	return idenfySuffix, nil
}

func GetChainNetworkName(substrateClient substrate.SubstrateClient) (string, error) {
	chainName, err := substrateClient.GetChainName()
	if err != nil {
		return "", err
	}
	chainNameParts := strings.Split(chainName, " ")
	chainNetworkName := strings.ToLower(chainNameParts[len(chainNameParts)-1])
	return chainNetworkName, nil
}

// -----------------------------
// Token related methods
// -----------------------------
func (s *KYCService) GetOrCreateVerificationToken(ctx context.Context, clientID string) (*models.Token, bool, error) {
	isVerified, err := s.IsUserVerified(ctx, clientID)
	if err != nil {
		s.logger.Error("Error checking if user is verified", "clientID", clientID, "error", err)
		return nil, false, errors.NewInternalError("getting verification status from database", err)
	}
	if isVerified {
		return nil, false, errors.NewConflictError("user already verified", nil)
	}
	if s.config.AlwaysVerifiedIDsOnly {
		// If AlwaysVerifiedIDsOnly mode is active, KYC token creation is disabled on this network.
		return nil, false, errors.NewForbiddenError("KYC is disabled while AlwaysVerifiedIDsOnly mode is active", nil)
	}
	token, err_ := s.tokenRepo.GetToken(ctx, clientID)
	if err_ != nil {
		s.logger.Error("Error getting token from database", "clientID", clientID, "error", err_)
		return nil, false, errors.NewInternalError("getting token from database", err_)
	}
	// check if token is found and not expired
	if token != nil {
		duration := time.Since(token.CreatedAt)
		if duration < time.Duration(token.ExpiryTime)*time.Second {
			remainingTime := time.Duration(token.ExpiryTime)*time.Second - duration
			token.ExpiryTime = int(remainingTime.Seconds())
			return token, false, nil
		}
	}

	// check if user account balance satisfies the minimum required balance, return an error if not
	hasRequiredBalance, err_ := s.AccountHasRequiredBalance(ctx, clientID)
	if err_ != nil {
		s.logger.Error("Error checking if user account has required balance", "clientID", clientID, "error", err_)
		return nil, false, errors.NewExternalError("checking if user account has required balance", err_)
	}
	if !hasRequiredBalance {
		requiredBalance := s.config.MinBalanceToVerifyAccount / TFT_CONVERSION_FACTOR
		return nil, false, errors.NewNotSufficientBalanceError(fmt.Sprintf("account does not have the minimum required balance to verify (%d) TFT", requiredBalance), nil)
	}
	// prefix clientID with tfchain network prefix
	uniqueClientID := clientID + ":" + s.IdenfySuffix
	newToken, err_ := s.idenfy.CreateVerificationSession(ctx, uniqueClientID)
	if err_ != nil {
		s.logger.Error("Error creating iDenfy verification session", "clientID", clientID, "uniqueClientID", uniqueClientID, "error", err_)
		return nil, false, errors.NewExternalError("creating iDenfy verification session", err_)
	}
	// save the token with the original clientID
	newToken.ClientID = clientID
	err_ = s.tokenRepo.SaveToken(ctx, &newToken)
	if err_ != nil {
		s.logger.Error("Error saving verification token to database", "clientID", clientID, "error", err_)
	}

	return &newToken, true, nil
}

func (s *KYCService) DeleteToken(ctx context.Context, clientID string, scanRef string) error {

	err := s.tokenRepo.DeleteToken(ctx, clientID, scanRef)
	if err != nil {
		s.logger.Error("Error deleting verification token from database", "clientID", clientID, "scanRef", scanRef, "error", err)
		return errors.NewInternalError("deleting verification token from database", err)
	}
	return nil
}

func (s *KYCService) AccountHasRequiredBalance(ctx context.Context, address string) (bool, error) {
	if s.config.MinBalanceToVerifyAccount == 0 {
		s.logger.Warn("Minimum balance to verify account is 0 which is not recommended", "address", address)
		return true, nil
	}
	balance, err := s.substrate.GetAccountBalance(address)
	if err != nil {
		s.logger.Error("Error getting account balance", "address", address, "error", err)
		return false, errors.NewExternalError("getting account balance", err)
	}
	return balance >= s.config.MinBalanceToVerifyAccount, nil
}

// -----------------------------
// Verifications related methods
// -----------------------------
// GetVerificationData returns the verification data to show to the client itself.
// While KYC is disabled it returns an empty record instead of the stored data.
func (s *KYCService) GetVerificationData(ctx context.Context, clientID string) (*models.Verification, error) {
	if s.config.Disabled {
		s.logger.Debug("KYC is disabled: returning empty verification record", "clientID", clientID)
		final := true
		return &models.Verification{
			Final:    &final,
			ClientID: clientID,
			Data: models.PersonData{
				DuplicateFaces:    []string{},
				DuplicateDocFaces: []string{},
			},
		}, nil
	}
	return s.getStoredVerification(ctx, clientID)
}

// getStoredVerification reads the stored verification record. Decisions about
// verification status must use this, never the client-facing GetVerificationData.
func (s *KYCService) getStoredVerification(ctx context.Context, clientID string) (*models.Verification, error) {
	verification, err := s.verificationRepo.GetVerification(ctx, clientID)
	if err != nil {
		s.logger.Error("Error getting verification from database", "clientID", clientID, "error", err)
		return nil, errors.NewInternalError("getting verification from database", err)
	}
	return verification, nil
}

func (s *KYCService) GetVerificationStatus(ctx context.Context, clientID string) (*models.VerificationOutcome, error) {
	if s.config.Disabled {
		s.logger.Debug("KYC is disabled: reporting client as verified", "clientID", clientID)
		final := true
		return &models.VerificationOutcome{
			Final:    &final,
			ClientID: clientID,
			Outcome:  models.OutcomeApproved,
		}, nil
	}
	// check first if the clientID is in alwaysVerifiedAddresses
	if s.config.AlwaysVerifiedIDs != nil && slices.Contains(s.config.AlwaysVerifiedIDs, clientID) {
		final := true
		s.logger.Info("ClientID is in always verified addresses. skipping verification", "clientID", clientID)
		return &models.VerificationOutcome{
			Final:     &final,
			ClientID:  clientID,
			IdenfyRef: "",
			Outcome:   models.OutcomeApproved,
		}, nil
	}
	if s.config.AlwaysVerifiedIDsOnly {
		return nil, nil
	}
	verification, err := s.getVerificationOutcome(ctx, clientID)
	if err != nil {
		s.logger.Error("Error getting verification from database", "clientID", clientID, "error", err)
		return nil, err
	}
	if verification == nil {
		return nil, nil
	}
	return verification, nil
}

func (s *KYCService) GetVerificationStatusByTwinID(ctx context.Context, twinID uint32) (*models.VerificationOutcome, error) {
	// get the address from the twinID
	address, err := s.substrate.GetAddressByTwinID(twinID)
	if err != nil {
		s.logger.Error("Error getting address from twinID", "twinID", twinID, "error", err)
		return nil, errors.NewExternalError("looking up twinID address from TFChain", err)
	}
	return s.GetVerificationStatus(ctx, address)
}

func (s *KYCService) ProcessVerificationResult(ctx context.Context, body []byte, sigHeader string, result models.Verification) error {
	err := s.verifyIdenfyCallbackSignature(ctx, body, sigHeader)
	if err != nil {
		return err
	}
	clientID, err := s.processClientID(result.ClientID)
	if err != nil {
		return err
	}
	// delete the token with the same clientID and same scanRef
	result.ClientID = clientID

	err = s.tokenRepo.DeleteToken(ctx, result.ClientID, result.IdenfyRef)
	if err != nil {
		s.logger.Warn("Error deleting verification token from database", "clientID", result.ClientID, "scanRef", result.IdenfyRef, "error", err)
	}
	// if the verification status is EXPIRED, we don't need to save it
	if result.Status.Overall != nil && *result.Status.Overall != models.Overall("EXPIRED") {
		// remove idenfy suffix from clientID
		err = s.verificationRepo.SaveVerification(ctx, &result)
		if err != nil {
			s.logger.Error("Error saving verification to database", "clientID", result.ClientID, "scanRef", result.IdenfyRef, "error", err)
			return errors.NewInternalError("saving verification to database", err)
		}
	}
	s.logger.Info("Verification result processed successfully", "result", result)
	return nil
}

func (s *KYCService) ProcessDocExpirationNotification(ctx context.Context, body []byte, sigHeader string, notification models.DocExpirationNotification) error {
	err := s.verifyIdenfyCallbackSignature(ctx, body, sigHeader)
	if err != nil {
		return err
	}
	clientID, err := s.processClientID(notification.ClientID)
	if err != nil {
		return err
	}
	// Update verification that matches the same clientID and scanref with expiration status
	err = s.verificationRepo.UpdateExpirationStatus(ctx, clientID, notification.ScanRef, notification.ExpirationThreshold)
	if err != nil {
		s.logger.Error("Error updating expiration status",
			"clientID", clientID,
			"status", notification.ExpirationThreshold,
			"error", err)
		return errors.NewInternalError("updating expiration status", err)
	}

	s.logger.Info("Updated document expiration status",
		"clientID", clientID,
		"status", notification.ExpirationThreshold)
	return nil
}

func (s *KYCService) verifyIdenfyCallbackSignature(ctx context.Context, body []byte, sigHeader string) error {
	err := s.idenfy.VerifyCallbackSignature(ctx, body, sigHeader)
	if err != nil {
		s.logger.Error("Error verifying callback signature", "sigHeader", sigHeader, "error", err)
		return errors.NewAuthorizationError("verifying callback signature", err)
	}
	return nil
}

func (s *KYCService) processClientID(clientID string) (string, error) {
	strippedClientID, actualSuffix, found := strings.Cut(clientID, ":")
	// defensively check if the clientID has a network suffix that is different from the expected one
	if !found {
		s.logger.Warn("clientID have no network suffix", "clientID", clientID)
	} else if actualSuffix != s.IdenfySuffix {
		s.logger.Warn("clientID has different network suffix", "clientID", clientID, "expectedSuffix", s.IdenfySuffix, "actualSuffix", actualSuffix)
	}
	return strippedClientID, nil
}

// IsUserVerified checks if a user is directly KYC-verified
// While KYC is disabled every user counts as verified, so no new verification
// session is started.
func (s *KYCService) IsUserVerified(ctx context.Context, clientID string) (bool, error) {
	if s.config.Disabled {
		s.logger.Debug("KYC is disabled: treating user as verified", "clientID", clientID)
		return true, nil
	}
	verification, err := s.getVerificationOutcome(ctx, clientID)
	if err != nil {
		return false, err
	}
	if verification == nil {
		return false, nil
	}
	return verification.Outcome == models.OutcomeApproved, nil
}

func (s *KYCService) getVerificationOutcome(ctx context.Context, clientID string) (*models.VerificationOutcome, error) {
	verification, err := s.getStoredVerification(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if verification == nil {
		// does user have a sponsorship?
		sponsorship, err := s.sponsorshipRepo.GetBySponsee(ctx, clientID)
		if err != nil {
			s.logger.Error("Error checking sponsorship for user", "clientID", clientID, "error", err)
			return nil, errors.NewInternalError("checking sponsorship for user", err)
		}
		if sponsorship != nil {
			// User is sponsored, check if the sponsor is verified
			sponsorVerification, err := s.getStoredVerification(ctx, sponsorship.SponsorClientID)
			if err != nil {
				s.logger.Error("Error checking sponsor verification status", "sponsorClientID", sponsorship.SponsorClientID, "error", err)
				return nil, errors.NewInternalError("checking sponsor verification status", err)
			}
			if sponsorVerification != nil {
				// return the verification outcome of the sponsor
				return sponsorVerification.ToOutcome(*s.config), nil
			}
			s.logger.Warn("User is sponsored by an unverified sponsor", "sponseeClientID", clientID, "sponsorClientID", sponsorship.SponsorClientID)
			return nil, nil // User is sponsored by an unverified sponsor
		}
		return nil, nil // User is not sponsored
	}

	return verification.ToOutcome(*s.config), nil
}

// -----------------------------
// Sponsorship related methods
// -----------------------------
// CreateSponsorship creates a new sponsorship between a sponsor and sponsee
func (s *KYCService) CreateSponsorship(ctx context.Context, sponsorClientID, sponseeClientID string) (*models.Sponsorship, error) {
	// While KYC is disabled nobody can be checked as a sponsor, so refuse rather
	// than store sponsorships that would outlive the outage.
	if s.config.Disabled {
		return nil, errors.NewForbiddenError("KYC is disabled: new sponsorships are not accepted", nil)
	}
	// Check if sponsor is directly KYC-verified
	sponsorVerification, err := s.getStoredVerification(ctx, sponsorClientID)
	if err != nil {
		return nil, fmt.Errorf("checking sponsor verification status: %w", err)
	}
	if sponsorVerification == nil || sponsorVerification.ToOutcome(*s.config).Outcome != models.OutcomeApproved {
		return nil, errors.NewAuthorizationError("sponsor is not KYC-verified", nil)
	}

	// Check if sponsee is already sponsored
	existingSponsorship, err := s.sponsorshipRepo.GetBySponsee(ctx, sponseeClientID)
	if err != nil {
		return nil, fmt.Errorf("checking existing sponsorship: %w", err)
	}
	if existingSponsorship != nil {
		return nil, errors.NewConflictError("sponsee is already sponsored", nil)
	}

	// Create the sponsorship
	sponsorship := &models.Sponsorship{
		SponsorClientID: sponsorClientID,
		SponseeClientID: sponseeClientID,
		CreatedAt:       time.Now(),
	}

	if err := s.sponsorshipRepo.Create(ctx, sponsorship); err != nil {
		return nil, fmt.Errorf("creating sponsorship: %w", err)
	}

	return sponsorship, nil
}

// GetSponsorships returns all active sponsorships for a given sponsor
func (s *KYCService) GetSponsorshipsBySponsor(ctx context.Context, sponsorTwinID uint32, limit, offset int64) ([]*models.Sponsorship, int64, error) {
	// get the address from the twinID
	address, err := s.substrate.GetAddressByTwinID(sponsorTwinID)
	if err != nil {
		return nil, 0, fmt.Errorf("getting address from twinID: %w", err)
	}

	return s.GetSponsorshipsBySponsorClientID(ctx, address, limit, offset)
}

// GetSponsorshipsBySponsorClientID returns all active sponsorships for a given sponsor client ID
func (s *KYCService) GetSponsorshipsBySponsorClientID(ctx context.Context, sponsorClientID string, limit, offset int64) ([]*models.Sponsorship, int64, error) {
	pagination := repository.PaginationParams{
		Limit:  limit,
		Offset: offset,
	}
	return s.sponsorshipRepo.GetBySponsor(ctx, sponsorClientID, pagination)
}

// GetSponsorshipBySponsee returns the active sponsorship for a given sponsee
func (s *KYCService) GetSponsorshipBySponsee(ctx context.Context, sponseeTwinID uint32) (*models.Sponsorship, error) {
	// get the address from the twinID
	address, err := s.substrate.GetAddressByTwinID(sponseeTwinID)
	if err != nil {
		return nil, fmt.Errorf("getting address from twinID: %w", err)
	}
	return s.GetSponsorshipBySponseeClientID(ctx, address)
}

// GetSponsorshipBySponseeClientID returns the active sponsorship for a given sponsee client ID
func (s *KYCService) GetSponsorshipBySponseeClientID(ctx context.Context, sponseeClientID string) (*models.Sponsorship, error) {
	return s.sponsorshipRepo.GetBySponsee(ctx, sponseeClientID)
}

// ListAllSponsorships returns a paginated list of all active sponsorships
// limit: maximum number of sponsorships to return (default: 100, max: 1000)
// offset: number of sponsorships to skip (default: 0)
func (s *KYCService) ListAllSponsorships(ctx context.Context, limit, offset int64) ([]*models.Sponsorship, int64, error) {
	pagination := repository.PaginationParams{
		Limit:  limit,
		Offset: offset,
	}

	return s.sponsorshipRepo.ListAll(ctx, pagination)
}
