package chconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSMTPConfigIsConfigured(t *testing.T) {
	testCases := []struct {
		name string
		smtp SMTPConfig
		want bool
	}{
		{name: "empty section", smtp: SMTPConfig{}, want: false},
		{name: "secure alone is not a configuration", smtp: SMTPConfig{Secure: true}, want: false},
		{name: "server only", smtp: SMTPConfig{Server: "smtp.example.com:587"}, want: true},
		{name: "sender only", smtp: SMTPConfig{SenderEmail: "ops@example.com"}, want: true},
		{name: "credentials only", smtp: SMTPConfig{AuthUsername: "ops", AuthPassword: "x"}, want: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.smtp.IsConfigured())
		})
	}
}
