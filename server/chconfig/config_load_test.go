package chconfig

import (
	"os"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	chshare "github.com/proximile/proxiport/share"
)

func TestLoadingExampleConf(t *testing.T) {
	var (
		viperCfg *viper.Viper
		cfg      = &Config{}
	)
	viperCfg = viper.New()
	viperCfg.SetConfigType("toml")
	viperCfg.SetConfigFile("../../proxiportd.example.conf")
	path, err := os.Getwd()
	require.NoError(t, err)
	t.Logf("Testing example config %s.proxiportd.example.conf\n", path)
	err = chshare.DecodeViperConfig(viperCfg, cfg, nil)
	require.NoError(t, err)
	assert.Equal(t, "<YOUR_SEED>", cfg.Server.KeySeed)
	assert.Equal(t, "clientAuth1:1234", cfg.Server.Auth)
	assert.Equal(t, "/var/lib/proxiport", cfg.Server.DataDir)
}

