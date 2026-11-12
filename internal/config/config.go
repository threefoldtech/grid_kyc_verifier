/*
Package config contains the configuration for the application.
This layer is responsible for loading the configuration from the environment variables and validating it.
*/
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	MongoDB      MongoDB
	Server       Server
	Idenfy       Idenfy
	TFChain      TFChain
	Verification Verification
	IPLimiter    IPLimiter
	IDLimiter    IDLimiter
	Challenge    Challenge
	Log          Log
}

type MongoDB struct {
	URI          string `env:"MONGO_URI" env-default:"mongodb://localhost:27017"`
	DatabaseName string `env:"DATABASE_NAME" env-default:"tf-kyc-db"`
}
type Server struct {
	Port string `env:"PORT" env-default:"8080"`
}
type Idenfy struct {
	APIKey          string   `env:"IDENFY_API_KEY" env-required:"true"`
	APISecret       string   `env:"IDENFY_API_SECRET" env-required:"true"`
	BaseURL         string   `env:"IDENFY_BASE_URL" env-default:"https://ivs.idenfy.com"`
	CallbackSignKey string   `env:"IDENFY_CALLBACK_SIGN_KEY" env-required:"true"`
	WhitelistedIPs  []string `env:"IDENFY_WHITELISTED_IPS" env-separator:","`
	DevMode         bool     `env:"IDENFY_DEV_MODE" env-default:"false"`
	CallbackUrl     string   `env:"IDENFY_CALLBACK_URL" env-required:"false"`
	Namespace       string   `env:"IDENFY_NAMESPACE" env-default:""`
}

// implement getter for Idenfy
func (c *Idenfy) GetCallbackUrl() string {
	return c.CallbackUrl
}
func (c *Idenfy) GetNamespace() string {
	return c.Namespace
}
func (c *Idenfy) GetDevMode() bool {
	return c.DevMode
}
func (c *Idenfy) GetWhitelistedIPs() []string {
	return c.WhitelistedIPs
}
func (c *Idenfy) GetAPIKey() string {
	return c.APIKey
}
func (c *Idenfy) GetAPISecret() string {
	return c.APISecret
}
func (c *Idenfy) GetBaseURL() string {
	return c.BaseURL
}
func (c *Idenfy) GetCallbackSignKey() string {
	return c.CallbackSignKey
}

type TFChain struct {
	WsProviderURL []string `env:"TFCHAIN_WS_PROVIDER_URL" env-default:"wss://tfchain.grid.tf" env-separator:","`
}

// implement getter for TFChain
func (c *TFChain) GetWsProviderURL() []string {
	return c.WsProviderURL
}

type Verification struct {
	SuspiciousVerificationOutcome string   `env:"VERIFICATION_SUSPICIOUS_VERIFICATION_OUTCOME" env-default:"APPROVED"`
	ExpiredDocumentOutcome        string   `env:"VERIFICATION_EXPIRED_DOCUMENT_OUTCOME" env-default:"REJECTED"`
	MinBalanceToVerifyAccount     uint64   `env:"VERIFICATION_MIN_BALANCE_TO_VERIFY_ACCOUNT" env-default:"10000000"`
	AlwaysVerifiedIDs             []string `env:"VERIFICATION_ALWAYS_VERIFIED_IDS" env-separator:","`
	AlwaysVerifiedIDsOnly         bool     `env:"VERIFICATION_ALWAYS_VERIFIED_IDS_ONLY" env-default:"false"`
	// Disabled turns KYC off: every client is reported as verified, no new
	// verification sessions start, no stored verification data is served,
	// and new sponsorships are refused.
	Disabled bool `env:"VERIFICATION_DISABLED" env-default:"false"`
}
type IPLimiter struct {
	MaxTokenRequests uint `env:"IP_LIMITER_MAX_TOKEN_REQUESTS" env-default:"4"`
	TokenExpiration  uint `env:"IP_LIMITER_TOKEN_EXPIRATION" env-default:"1440"`
}
type IDLimiter struct {
	MaxTokenRequests uint `env:"ID_LIMITER_MAX_TOKEN_REQUESTS" env-default:"4"`
	TokenExpiration  uint `env:"ID_LIMITER_TOKEN_EXPIRATION" env-default:"1440"`
}
type Log struct {
	Debug bool `env:"DEBUG" env-default:"false"`
}
type Challenge struct {
	Window int64  `env:"CHALLENGE_WINDOW" env-default:"8"`
	Domain string `env:"CHALLENGE_DOMAIN" env-required:"true"`
}

func LoadConfigFromEnv() (*Config, error) {
	cfg := &Config{}
	err := cleanenv.ReadEnv(cfg)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	// cfg.Validate()
	return cfg, nil
}

func (c Config) GetPublicConfig() Config {
	// deducting the secret fields
	config := c
	config.Idenfy.APIKey = "[REDACTED]"
	config.Idenfy.APISecret = "[REDACTED]"
	config.Idenfy.CallbackSignKey = "[REDACTED]"
	config.MongoDB.URI = "[REDACTED]"
	return config
}

// validate config
func (c *Config) Validate() error {
	// iDenfy base URL should be https://ivs.idenfy.com. This is the only supported base URL for now.
	if c.Idenfy.BaseURL != "https://ivs.idenfy.com" {
		return errors.New("invalid iDenfy base URL. it should be https://ivs.idenfy.com")
	}
	// CallbackUrl should be valid URL
	parsedCallbackUrl, err := url.ParseRequestURI(c.Idenfy.CallbackUrl)
	if err != nil {
		return errors.New("invalid CallbackUrl")
	}
	// CallbackSignKey should not be empty
	if len(c.Idenfy.CallbackSignKey) < 16 {
		return errors.New("invalid callbackSignKey. it should be at least 16 characters long")
	}

	// WsProviderURL should not be empty
	if len(c.TFChain.WsProviderURL) == 0 {
		return errors.New("empty WsProviderURL")
	}
	// WsProviderURL should be valid URL and start with wss://
	for _, substrate_url := range c.TFChain.WsProviderURL {
		if u, err := url.ParseRequestURI(substrate_url); err != nil || u.Scheme != "wss" {
			return errors.New("invalid WsProviderURL")
		}
	}

	// domain should not be empty and same as domain in CallbackUrl
	if parsedCallbackUrl.Host != c.Challenge.Domain {
		return errors.New("invalid Challenge Domain. It should be same as domain in CallbackUrl")
	}
	// Window should be greater than 2
	if c.Challenge.Window < 2 {
		return errors.New("invalid Challenge Window. It should be greater than 2 otherwise it will be too short and verification can fail in slow networks")
	}
	// SuspiciousVerificationOutcome should be either APPROVED or REJECTED
	if !slices.Contains([]string{"APPROVED", "REJECTED"}, c.Verification.SuspiciousVerificationOutcome) {
		return errors.New("invalid SuspiciousVerificationOutcome. should be either APPROVED or REJECTED")
	}
	// ExpiredDocumentOutcome should be either APPROVED or REJECTED
	if !slices.Contains([]string{"APPROVED", "REJECTED"}, c.Verification.ExpiredDocumentOutcome) {
		return errors.New("invalid ExpiredDocumentOutcome. should be either APPROVED or REJECTED")
	}
	// AlwaysVerifiedIDsOnly cannot be true if the AlwaysVerifiedIDs list is empty
	if c.Verification.AlwaysVerifiedIDsOnly && len(c.Verification.AlwaysVerifiedIDs) == 0 {
		return errors.New("AlwaysVerifiedIDsOnly cannot be true if the AlwaysVerifiedIDs list is empty. Either set AlwaysVerifiedIDsOnly to false, or add at least one allowed ID to the AlwaysVerifiedIDs list")
	}
	// MinBalanceToVerifyAccount
	if c.Verification.MinBalanceToVerifyAccount < 20000000 {
		slog.Warn("Verification MinBalanceToVerifyAccount is less than 20000000. This is not recommended and can lead to security issues. If you are sure about this, you can ignore this message.")
	}
	// DevMode
	if c.Idenfy.DevMode {
		slog.Warn("iDenfy DevMode is enabled. This is not intended for environments other than development. If you are sure about this, you can ignore this message.")
	}
	// Namespace
	if c.Idenfy.Namespace != "" {
		slog.Warn("iDenfy Namespace is set. This ideally should be empty. If you are sure about this, you can ignore this message.")
	}
	return nil
}
