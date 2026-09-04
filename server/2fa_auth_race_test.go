package chserver

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/api/message"
	"github.com/proximile/proxiport/server/api/users"
)

// TestValidateTotPCodeConcurrent is a regression test for a data race in the
// 2FA-verify path: the login-session entry was deleted while only a read lock
// was held, so two callers deleting different keys at once mutated the map
// concurrently and could crash the daemon with "fatal error: concurrent map
// writes". Run with -race. It also exercises the writer path
// (SetTotPLoginSession) racing the validator's delete.
func TestValidateTotPCodeConcurrent(t *testing.T) {
	const userCount = 8
	const iterations = 300

	tfaService := NewTwoFAService(100, time.Second, &MockUsersService{}, &message.ServiceMock{})

	usrs := make([]*users.User, userCount)
	codes := make([]string, userCount)
	for i := 0; i < userCount; i++ {
		usr := &users.User{Username: "user-" + strconv.Itoa(i)}
		totP, err := GenerateTotPSecretKey(&TotPInput{Issuer: "iss", AccountName: usr.Username})
		require.NoError(t, err)
		StoreTotPCodeInUser(usr, totP)
		code, err := totp.GenerateCode(totP.Secret, time.Now())
		require.NoError(t, err)
		usrs[i] = usr
		codes[i] = code
	}

	var wg sync.WaitGroup
	for i := 0; i < userCount; i++ {
		usr := usrs[i]
		code := codes[i]

		// Validator: on success this consumes (deletes) the session entry.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = tfaService.ValidateTotPCode(usr, code)
			}
		}()

		// Writer: continuously (re)creates the login session entry.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				tfaService.SetTotPLoginSession(usr.Username, time.Minute)
			}
		}()
	}
	wg.Wait()
}
