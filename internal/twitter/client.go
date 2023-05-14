package twitter

import (
	"fmt"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/tidwall/gjson"
	"io/ioutil"
	"moff.io/moff-social/internal/config"
	"moff.io/moff-social/pkg/errors"
	"net/http"
	"sync"
)

type Client struct {
	cli            *twitter.Client
	accessToken    string
	tokenRefresher *tokenRefresher
}

var (
	initClientOnce sync.Once
	internalClient *Client
)

func NewClient() *Client {
	initClientOnce.Do(func() {
		internalClient = &Client{}
	})
	return internalClient
}

func (in *Client) Apply(conf *config.Configuration) {
	in.checkConfiguration(conf)
	in.tokenRefresher = newTokenRefresher(conf.Twitter.ApiKey, conf.Twitter.ApiSecret, conf.Twitter.RefreshTokenURL)
	if err := in.RefreshAccessToken(); err != nil {
		panic(err)
	}
	in.cli = &twitter.Client{
		Authorizer: in,
		Client:     http.DefaultClient,
		Host:       conf.Twitter.ApiURL,
	}
}

func (in *Client) checkConfiguration(conf *config.Configuration) {
	if conf.Twitter.ApiURL == "" {
		panic("twitter api url not configured")
	}
	if conf.Twitter.ApiKey == "" {
		panic("twitter api key not configured")
	}
	if conf.Twitter.ApiSecret == "" {
		panic("twitter api secret not configured")
	}
	if conf.Twitter.RefreshTokenURL == "" {
		panic("twitter refresh token url not configured")
	}
}

func (in *Client) RefreshAccessToken() error {
	token, err := in.tokenRefresher.Refresh()
	if err != nil {
		return err
	}
	in.accessToken = token
	return nil
}

func (in *Client) Add(req *http.Request) {
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", in.accessToken))
}

type tokenRefresher struct {
	apiKey, apiSecret, refreshURL string
}

func newTokenRefresher(apiKey, apiSecret, refreshURL string) *tokenRefresher {
	return &tokenRefresher{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		refreshURL: refreshURL,
	}
}

func (in *tokenRefresher) Refresh() (string, error) {
	post, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%v?grant_type=client_credentials", in.refreshURL), nil)
	if err != nil {
		return "", errors.WrapAndReport(err, "create new refresh token request")
	}
	post.Header.Add("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	post.SetBasicAuth(in.apiKey, in.apiSecret)
	response, err := http.DefaultClient.Do(post)
	if err != nil {
		return "", errors.WrapAndReport(err, "send refresh token request to twitter")
	}
	defer response.Body.Close()
	result, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", errors.WrapAndReport(err, "read refresh token response")
	}
	token := gjson.Get(string(result), "access_token").String()
	if token == "" {
		return "", errors.ErrorfAndReport("access token not found from response %v", string(result))
	}
	return token, nil
}
