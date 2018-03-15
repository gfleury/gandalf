package bitbucketapi

import (
	"context"

	"github.com/gfleury/go-bitbucket-v1"
	"github.com/tsuru/config"
)

const (
	DefaultAPIBaseURL = "https://stash.trustyou.com/rest"
)

func client(currentctx context.Context) (*bitbucketv1.APIClient, error) {
	url, username, password := APIConfig()
	basicAuth := bitbucketv1.BasicAuth{UserName: username, Password: password}
	currentctx = context.WithValue(currentctx, bitbucketv1.ContextBasicAuth, basicAuth)

	client := bitbucketv1.NewAPIClient(
		currentctx,
		bitbucketv1.NewConfiguration(url),
	)

	return client, nil
}

func Client(ctx context.Context) (*bitbucketv1.APIClient, error) {
	return client(ctx)
}

func APIConfig() (string, string, string) {
	url, _ := config.GetString("api:url")
	if url == "" {
		url = DefaultAPIBaseURL
	}
	username, _ := config.GetString("api:username")
	password, _ := config.GetString("api:password")

	return url, username, password
}
