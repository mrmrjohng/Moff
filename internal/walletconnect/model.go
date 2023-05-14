package walletconnect

import (
	"encoding/json"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strings"
)

type Wallet struct {
	Meta     clientMeta `json:"peerMeta"`
	ChainID  int        `json:"chainId"`
	Accounts []string   `json:"accounts"`
	PeerID   string     `json:"peerId"`

	// approved or rejected
	approved bool
	// signed or rejected
	signed bool
}

func (in *Wallet) Confirmed() bool {
	return in.approved && in.signed
}

func (in *Wallet) Approved() bool {
	return in.approved
}

func (in *Wallet) Signed() bool {
	return in.signed
}

type wcMessagePayload struct {
	Data string `json:"data"`
	Hmac string `json:"hmac"`
	IV   string `json:"iv"`
}

func newWCMessagePayloadFromBytes(data []byte) (*wcMessagePayload, error) {
	var payload wcMessagePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, errors.WrapAndReport(err, "unmarshal wallet connect message payload")
	}
	return &payload, nil
}

func (e *wcMessagePayload) Marshal() string {
	s, err := json.Marshal(e)
	if err != nil {
		log.Errorf("marshal:%v", err)
	}
	return string(s)
}

type peer struct {
	PeerID   string      `json:"peerId"`
	PeerMeta clientMeta  `json:"peerMeta"`
	ChainID  interface{} `json:"chainId"`
}

type clientMeta struct {
	Description string   `json:"description"`
	URL         string   `json:"url"`
	Icons       []string `json:"icons"`
	Name        string   `json:"name"`
}

type wcMessage struct {
	Topic string `json:"topic"`
	// pub sub
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Silent  bool   `json:"silent"`
}

func newWCMessageFromBytes(data []byte) (*wcMessage, error) {
	var msg wcMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, errors.WrapAndReport(err, "unmarshal wallet connect message")
	}
	return &msg, nil
}

func (msg *wcMessage) Marshal() []byte {
	bytes, _ := json.Marshal(msg)
	return bytes
}

type jsonRpcRequest struct {
	Id      int64         `json:"id"`
	JSONRpc string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

func newJSONRpcRequest(method string, params ...interface{}) *jsonRpcRequest {
	r := &jsonRpcRequest{
		Id:      payloadID(),
		JSONRpc: "2.0",
		Method:  method,
		Params:  []interface{}{},
	}
	if len(params) > 0 {
		r.Params = params
	}
	return r
}

func (e *jsonRpcRequest) Marshal() string {
	s, err := json.Marshal(e)
	if err != nil {
		log.Errorf("marshal:%v", err)
	}
	return string(s)
}

func (e *jsonRpcRequest) IsSilentPayload() bool {
	if strings.HasPrefix(e.Method, "wc_") {
		return true
	}
	return false
}
