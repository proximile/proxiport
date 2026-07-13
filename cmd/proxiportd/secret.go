package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	chshare "github.com/proximile/proxiport/share"
)

// The `secret` command turns a plaintext config value into the encrypted form
// the server accepts in its config file. Sensitive settings — key_seed,
// jwt_secret, the auth credentials — can then live on disk as ciphertext that
// is worthless without the key provider's DEK.
var (
	secretCmd = &cobra.Command{
		Use:   "secret",
		Short: "protect config secrets",
		Long: "Encrypt config values under the key provider, so settings like key_seed and\n" +
			"jwt_secret are stored as ciphertext instead of in the clear.",
	}
	secretEncryptCmd = &cobra.Command{
		Use:   "encrypt",
		Short: "encrypt a config value",
		Long: "Read a value and print its encrypted form, which can be pasted into the config\n" +
			"file in place of the plaintext. Requires a [key_provider] section: the value is\n" +
			"encrypted under that provider's key, and only a server holding the same key can\n" +
			"read it back.\n\n" +
			"The value is read from stdin when it is piped in, otherwise it is prompted for\n" +
			"without echo. It is never written to disk or to the process arguments.",
		Example: "proxiportd -c /etc/proxiport/proxiportd.conf secret encrypt",
		Run: func(*cobra.Command, []string) {
			out, err := encryptSecret(os.Stdin, term.IsTerminal(int(os.Stdin.Fd())))
			if err != nil {
				log.Fatalf("%v", err)
			}
			fmt.Println(out)
		},
	}
)

func init() {
	RootCmd.AddCommand(secretCmd)
	secretCmd.AddCommand(secretEncryptCmd)

	// reset default usage func
	secretCmd.SetUsageFunc((&cobra.Command{}).UsageFunc())
}

// encryptSecret reads one secret value and returns its encrypted config form.
// Only the key provider section of the config is needed, so this works against
// a config that is otherwise incomplete — an operator can protect key_seed
// before the rest of the server is configured.
func encryptSecret(in *os.File, interactive bool) (string, error) {
	if *cfgPath != "" {
		viperCfg.SetConfigFile(*cfgPath)
	} else {
		viperCfg.AddConfigPath(".")
		viperCfg.SetConfigName("proxiportd.conf")
	}
	if err := chshare.DecodeViperConfig(viperCfg, cfg, nil); err != nil {
		return "", err
	}
	if err := cfg.KeyProvider.ParseAndValidate(); err != nil {
		return "", fmt.Errorf("invalid key provider: %w", err)
	}
	envelope := cfg.KeyProvider.Envelope()
	if !envelope.Enabled() {
		return "", errors.New("no key provider configured: add a [key_provider] section with a key before encrypting config secrets")
	}

	value, err := readSecretValue(in, interactive)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", errors.New("refusing to encrypt an empty value")
	}

	return envelope.Encrypt(value)
}

// readSecretValue takes the value from a pipe when there is one, and otherwise
// prompts for it twice without echoing, the way a passphrase is entered.
func readSecretValue(in *os.File, interactive bool) (string, error) {
	if !interactive {
		piped, err := io.ReadAll(in)
		if err != nil {
			return "", fmt.Errorf("failed to read the value from stdin: %w", err)
		}
		// A trailing newline is an artifact of how the value was piped in, not
		// part of the secret.
		return strings.TrimRight(string(piped), "\r\n"), nil
	}

	fmt.Print("Enter value: ")
	value, err := term.ReadPassword(int(in.Fd()))
	if err != nil {
		return "", err
	}
	fmt.Print("\nConfirm value: ")
	confirm, err := term.ReadPassword(int(in.Fd()))
	if err != nil {
		return "", err
	}
	fmt.Print("\n")

	if string(value) != string(confirm) {
		return "", errors.New("values do not match")
	}
	return string(value), nil
}
