package config

import (
	"testing"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerificationDisabledFromEnv(t *testing.T) {
	t.Run("defaults to false", func(t *testing.T) {
		var v Verification
		require.NoError(t, cleanenv.ReadEnv(&v))
		assert.False(t, v.Disabled)
	})

	t.Run("VERIFICATION_DISABLED=true turns it on", func(t *testing.T) {
		t.Setenv("VERIFICATION_DISABLED", "true")
		var v Verification
		require.NoError(t, cleanenv.ReadEnv(&v))
		assert.True(t, v.Disabled)
	})
}
