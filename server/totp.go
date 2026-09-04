package chserver

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/proximile/proxiport/server/api/users"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	DefaultTotPQrImageWidth  = 200
	DefaultTotPQrImageHeight = 200
)

type TotPKeyStatus uint

func (tks TotPKeyStatus) String() string {
	switch tks {
	case TotPKeyPending:
		return "pending"
	case TotPKeyExists:
		return "exists"
	default:
		return ""
	}
}

const (
	TotPKeyPending TotPKeyStatus = iota + 1
	TotPKeyExists
)

var defaultTotPGenerateOptions = totp.GenerateOpts{
	Period:     30,
	SecretSize: 20,
	Digits:     otp.DigitsSix,
	//as for now Microsoft Authenticator generate invalid codes for all algos rather than SHA1, so before changing it
	// make sure that it works with MS Authenticator
	Algorithm: otp.AlgorithmSHA1,
}

type TotPInput struct {
	Issuer      string
	AccountName string
}

type TotP struct {
	Secret        string   `json:"secret"`
	QRImageBase64 string   `json:"qr"`
	TotPKey       *otp.Key `json:"-"`
	baseURL       *url.URL
}

func NewTotP(key *otp.Key) (*TotP, error) {
	u, err := url.Parse(key.URL())
	if err != nil {
		return nil, err
	}

	imgBase64, err := generateImage(key)
	if err != nil {
		return nil, err
	}

	totP := &TotP{
		Secret:        key.Secret(),
		QRImageBase64: imgBase64,
		TotPKey:       key,
		baseURL:       u,
	}

	return totP, err
}

func (tp *TotP) Serialize() string {
	if tp == nil || tp.TotPKey == nil {
		return ""
	}

	return tp.TotPKey.URL()
}

func (tp *TotP) Algorithm() (otp.Algorithm, error) {
	algoStr := tp.baseURL.Query().Get("algorithm")
	if algoStr == "" {
		return 0, errors.New("empty algorithm value")
	}

	switch algoStr {
	case otp.AlgorithmSHA1.String():
		return otp.AlgorithmSHA1, nil
	case otp.AlgorithmSHA256.String():
		return otp.AlgorithmSHA256, nil
	case otp.AlgorithmSHA512.String():
		return otp.AlgorithmSHA512, nil
	case otp.AlgorithmMD5.String():
		return otp.AlgorithmMD5, nil
	default:
		return 0, fmt.Errorf("invalid/unsupported algorithm value %s", algoStr)
	}
}

func (tp *TotP) Digits() (otp.Digits, error) {
	digitsStr := tp.baseURL.Query().Get("digits")
	if digitsStr == "" {
		return 0, errors.New("empty digits value")
	}

	digitsInt, err := strconv.Atoi(digitsStr)
	if err != nil {
		return 0, fmt.Errorf("invalid digits value %s", digitsStr)
	}

	digits := otp.Digits(digitsInt)

	switch digits {
	case otp.DigitsSix:
		return digits, nil
	case otp.DigitsEight:
		return digits, nil
	default:
		return 0, fmt.Errorf("invalid digits value %s", digitsStr)
	}
}

func (tp *TotP) Scheme() string {
	return tp.baseURL.Scheme
}

func (tp *TotP) Valid() error {
	if tp.TotPKey == nil {
		return errors.New("empty totp secret key")
	}

	if tp.Scheme() != "otpauth" {
		return fmt.Errorf("unexpected totp key scheme %s, otpauth is expected", tp.Scheme())
	}

	if tp.TotPKey.Type() != "totp" {
		return fmt.Errorf("invalid totp key type %s, totp type is expected", tp.TotPKey.Type())
	}

	if tp.TotPKey.Secret() == "" {
		return errors.New("empty totp secret key")
	}

	if tp.TotPKey.Period() == 0 {
		return errors.New("zero totp secret validity period, a positive integer is expected")
	}

	_, err := tp.Algorithm()
	if err != nil {
		return err
	}

	_, err = tp.Digits()
	if err != nil {
		return err
	}

	return nil
}

func unSerializeTotP(rawKeyData string) (*TotP, error) {
	k, err := otp.NewKeyFromURL(rawKeyData)
	if err != nil {
		return nil, err
	}

	totP, err := NewTotP(k)
	if err != nil {
		return nil, err
	}

	return totP, totP.Valid()
}

func GetUsersTotPCode(usr *users.User) (*TotP, error) {
	if strings.TrimSpace(usr.TotP) == "" {
		return nil, nil
	}

	totP, err := unSerializeTotP(usr.TotP)

	if err != nil {
		return nil, fmt.Errorf("failed to convert '%s' to TotP secret data: %v", usr.TotP, err)
	}

	return totP, nil
}

func StoreTotPCodeInUser(usr *users.User, totP *TotP) {
	usr.TotP = totP.Serialize()
}

func CheckTotPCode(code string, totP *TotP) bool {
	_, ok := CheckTotPCodeStep(code, totP)
	return ok
}

// CheckTotPCodeStep validates a TotP code and reports which time step it
// matched. The caller needs the step to reject a replay: with the +-1 skew a
// single observed code stays valid for about 90 seconds, so without recording
// the last accepted step, a code that was phished or shoulder-surfed can be
// used a second time by anyone who also has the password.
func CheckTotPCodeStep(code string, totP *TotP) (step int64, ok bool) {
	algo, err := totP.Algorithm()
	if err != nil {
		return 0, false
	}

	digits, err := totP.Digits()
	if err != nil {
		return 0, false
	}

	period := int64(totP.TotPKey.Period())
	if period <= 0 {
		period = 30
	}

	// Skew 0 per candidate, walked manually, so a match identifies its step.
	// This is the same acceptance window as Skew 1 over a single call.
	validateOpts := totp.ValidateOpts{
		Period:    uint(period),
		Skew:      0,
		Digits:    digits,
		Algorithm: algo,
	}

	now := time.Now().UTC()
	for _, offset := range []int64{0, -1, 1} {
		at := now.Add(time.Duration(offset*period) * time.Second)
		isValid, err := totp.ValidateCustom(code, totP.Secret, at, validateOpts)
		if err == nil && isValid {
			return at.Unix() / period, true
		}
	}

	return 0, false
}

func GenerateTotPSecretKey(inpt *TotPInput) (*TotP, error) {
	genOpts := getDefaultTotPGenerateOptions()
	genOpts.Issuer = inpt.Issuer
	genOpts.AccountName = inpt.AccountName

	key, err := totp.Generate(genOpts)

	if err != nil {
		return nil, err
	}

	return NewTotP(key)
}

func generateImage(key *otp.Key) (imgBase64 string, err error) {
	var buf bytes.Buffer
	if key.URL() == "" {
		return "", nil
	}
	img, err := key.Image(DefaultTotPQrImageWidth, DefaultTotPQrImageHeight)
	if err != nil {
		return "", err
	}

	err = png.Encode(&buf, img)
	if err != nil {
		return "", err
	}

	imgBase64 = base64.StdEncoding.EncodeToString(buf.Bytes())

	return imgBase64, nil
}

func getDefaultTotPGenerateOptions() totp.GenerateOpts {
	return defaultTotPGenerateOptions
}
