package moralis

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"moff.io/moff-social/pkg/errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type Client interface {
	// GetAddressNfts 获取给定地址的nft列表
	GetAddressNfts(req *GetAddressNFTRequest) (*GetAddressNFTResponse, error)
}

type client struct {
	apiBaseURL string
	apiKey     string

	httpClient *http.Client
}

const (
	defaultTimeout = time.Second * 10
)

var (
	internalClient *client
	initOnce       sync.Once
)

func Init(apiKey string) {
	if apiKey == "" {
		panic("moralis api key not present")
	}
	initOnce.Do(func() {
		internalClient = &client{
			apiBaseURL: "https://deep-index.moralis.io/api/v2",
			apiKey:     apiKey,
			httpClient: &http.Client{
				Timeout: defaultTimeout,
			},
		}
	})
}

func NewClient() Client {
	if internalClient == nil {
		panic("moralis init operation not invoked")
	}
	return internalClient
}

type GetAddressNFTRequest struct {
	// ChainID 或者 ChainName 二者设置一个即可
	ChainID        int
	ChainName      string
	Limit          int
	Format         string
	TokenAddresses []string
	Cursor         string
	OwnerAddress   string
}

func (in *GetAddressNFTRequest) GetChain() string {
	if in.ChainName != "" {
		return string(in.ChainName)
	}
	return fmt.Sprintf("0x%x", in.ChainID)
}

func (in *GetAddressNFTRequest) FormatQuery() string {
	val := url.Values{}
	val.Set("chain", in.GetChain())
	val.Set("format", in.Format)
	val.Set("limit", strconv.Itoa(in.Limit))
	for _, addr := range in.TokenAddresses {
		val.Add("token_addresses", addr)
	}
	val.Set("cursor", in.Cursor)
	return val.Encode()
}

type GetAddressNFTResponse struct {
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
	Cursor   string        `json:"cursor"`
	Status   string        `json:"status"`
	Result   []*AddressNFT `json:"result"`
}

type AddressNFT struct {
	TokenAddress      string `json:"token_address"`
	TokenID           string `json:"token_id"`
	OwnerOf           string `json:"owner_of"`
	BlockNumber       string `json:"block_number"`
	BlockNumberMinted string `json:"block_number_minted"`
	TokenHash         string `json:"token_hash"`
	Amount            string `json:"amount"`
	ContractType      string `json:"contract_type"`
	Name              string `json:"name"`
	Symbol            string `json:"symbol"`
	TokenURI          string `json:"token_uri"`
	Metadata          string `json:"metadata"`
	SyncedAt          string `json:"synced_at"`
	LastTokenURISync  string `json:"last_token_uri_sync"`
	LastMetadataSync  string `json:"last_metadata_sync"`
}

func (c *client) GetAddressNfts(req *GetAddressNFTRequest) (*GetAddressNFTResponse, error) {
	path := fmt.Sprintf("/%s/nft?%s", req.OwnerAddress, req.FormatQuery())
	var out GetAddressNFTResponse
	if err := c.request(path, http.MethodGet, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) request(path, method string, out interface{}) error {
	req, err := http.NewRequest(method, c.apiBaseURL+path, nil)
	if err != nil {
		return errors.WrapAndReport(err, "create new http request")
	}

	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.WithStackAndReport(err)
	}

	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.WithStackAndReport(err)
	}
	if resp.StatusCode != http.StatusOK {
		return errors.ErrorfAndReport("request moralis:%v", string(b))
	}

	return json.Unmarshal(b, out)
}
