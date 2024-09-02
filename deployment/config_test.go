package deployment

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigIsValid(t *testing.T) {
	baseConfig := func() Config {
		return Config{
			MattermostDownloadURL: "https://latest.mattermost.com/mattermost-enterprise-linux",
			LoadTestDownloadURL:   "https://github.com/mattermost/mattermost-load-test-ng/releases/download/v1.20.0/mattermost-load-test-ng-v1.20.0-linux-amd64.tar.gz",
		}
	}

	t.Run("paths", func(t *testing.T) {
		t.Run("MattermostDownloadUrl can be an url", func(t *testing.T) {
			c := baseConfig()

			require.NoError(t, c.IsValid())
		})

		t.Run("MattermostDownloadUrl can be a path", func(t *testing.T) {
			c := baseConfig()
			c.MattermostDownloadURL = "file:///some/path"

			require.NoError(t, c.IsValid())
		})

		t.Run("MattermostDownloadUrl must be an url or a file", func(t *testing.T) {
			c := baseConfig()
			c.MattermostDownloadURL = "/some/path"

			require.Error(t, c.IsValid())
		})
	})

	t.Run("DBName is valid", func(t *testing.T) {
		t.Run("empty ClusterIdentifier and empty DBName is valid", func(t *testing.T) {
			c := baseConfig()
			c.TerraformDBSettings.ClusterIdentifier = ""
			c.TerraformDBSettings.DBName = ""

			require.NoError(t, c.IsValid())
		})

		t.Run("empty ClusterIdentifier and non-empty DBName is valid", func(t *testing.T) {
			c := baseConfig()
			c.TerraformDBSettings.ClusterIdentifier = ""
			c.TerraformDBSettings.DBName = "db"

			require.NoError(t, c.IsValid())
		})

		t.Run("non-empty ClusterIdentifier and empty DBName is not valid", func(t *testing.T) {
			c := baseConfig()
			c.TerraformDBSettings.ClusterIdentifier = "cluster"
			c.TerraformDBSettings.DBName = ""

			require.Error(t, c.IsValid())
		})

		t.Run("non-empty ClusterIdentifier and non-empty DBName is valid", func(t *testing.T) {
			c := baseConfig()
			c.TerraformDBSettings.ClusterIdentifier = "cluster"
			c.TerraformDBSettings.DBName = "db"

			require.NoError(t, c.IsValid())
		})
	})
}

func TestValidateElasticSearchConfig(t *testing.T) {
	baseValidConfig := func() Config {
		return Config{
			ClusterName:           "clustername",
			MattermostDownloadURL: "https://latest.mattermost.com/mattermost-enterprise-linux",
			LoadTestDownloadURL:   "https://github.com/mattermost/mattermost-load-test-ng/releases/download/v1.20.0/mattermost-load-test-ng-v1.20.0-linux-amd64.tar.gz",
			ElasticSearchSettings: ElasticSearchSettings{
				InstanceCount:      1,
				Version:            "OpenSearch_2.7",
				VpcID:              "vpc-01234567890abcdef",
				SnapshotRepository: "somerepo",
				SnapshotName:       "somename",
			},
		}
	}

	t.Run("valid config", func(t *testing.T) {
		cfg := baseValidConfig()
		require.NoError(t, cfg.validateElasticSearchConfig())
	})

	t.Run("valid instance count", func(t *testing.T) {
		cfg := baseValidConfig()

		cfg.ElasticSearchSettings.InstanceCount = 1
		require.NoError(t, cfg.validateElasticSearchConfig())

		cfg.ElasticSearchSettings.InstanceCount = 42
		require.NoError(t, cfg.validateElasticSearchConfig())
	})

	t.Run("invalid VPC ID", func(t *testing.T) {
		cfg := baseValidConfig()
		cfg.ElasticSearchSettings.VpcID = ""
		require.Error(t, cfg.validateElasticSearchConfig())
	})

	t.Run("invalid domain name for ES", func(t *testing.T) {
		cfg := baseValidConfig()
		cfg.ClusterName = "InvalidClusterNameForES!@#$"

		require.Error(t, cfg.validateElasticSearchConfig())
	})

	t.Run("invalid domain name for ES but validation passes because InstanceCount == 0", func(t *testing.T) {
		cfg := baseValidConfig()
		cfg.ClusterName = "InvalidClusterNameForES!@#$"
		cfg.ElasticSearchSettings.InstanceCount = 0

		require.NoError(t, cfg.validateElasticSearchConfig())
	})
}
