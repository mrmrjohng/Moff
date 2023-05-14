package walletconnect

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/skip2/go-qrcode"
	"github.com/tidwall/gjson"
	"go.uber.org/atomic"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"moff.io/moff-social/pkg/wallectconnect"
	"net/url"
	"strings"
	"time"
)

var (
	errSessionClosed = errors.New("session closed")
)

type client struct {
	ctx    context.Context
	cancel context.CancelFunc

	// None zero value means can not call CollectWallet again, you should recreate client instead.
	collectWalletCount atomic.Int64
	qrGenerated        bool

	readTimeout time.Duration
	conn        *websocket.Conn
	bridgeURL   string

	handshakeTopic string
	clientID       string
	encryptionKey  []byte
	payloadID      int64

	signMsg string
	wallet  *Wallet
}

func NewClient() ClientV1 {
	encryptionKey, _ := wallectconnect.GenerateRandomBytes(256 / 8)
	return &client{
		encryptionKey:  encryptionKey,
		payloadID:      payloadID(),
		bridgeURL:      wallectconnect.RandomBridgeURL(),
		handshakeTopic: uuid.NewString(),
		clientID:       uuid.NewString(),
		readTimeout:    time.Minute * 5,
		wallet:         &Wallet{},
	}
}

func (c *client) ConnectWallet(ctx context.Context, signMsg string, displayQRCode DisplayQRCodeFn) (*Wallet, error) {
	if !c.collectWalletCount.CAS(0, 1) {
		return nil, errors.NewWithReport("duplicate collect wallet")
	}
	if !c.qrGenerated {
		return nil, errors.NewWithReport("call GetQRCode first to display")
	}
	c.signMsg = signMsg
	c.ctx, c.cancel = context.WithCancel(ctx)
	defer c.cancel()
	if err := c.dialWS(ctx); err != nil {
		return nil, err
	}
	return c.interact(displayQRCode)
}

// GetQRCode 返回用户钱包连接的二维码.
func (c *client) GetQRCode() ([]byte, error) {
	uri := fmt.Sprintf("wc:%s@1?bridge=%s&key=%s",
		c.handshakeTopic, url.QueryEscape(c.bridgeURL), hex.EncodeToString(c.encryptionKey))
	log.Debugf("wallet connect - generated uri:%v", uri)
	err := qrcode.WriteFile(uri, qrcode.Medium, 256, "wallet_connect_qr.png")

	png, err := qrcode.Encode(uri, qrcode.Medium, 256)
	if err != nil {
		return nil, errors.WrapAndReport(err, "encode wallet connect qr code")
	}
	c.qrGenerated = true
	return png, nil
}

func (c *client) interact(displayQRCode DisplayQRCodeFn) (*Wallet, error) {
	defer c.close()
	if err := c.subscribeSession(); err != nil {
		return nil, err
	}
	if err := c.createSessionRequest(); err != nil {
		return nil, err
	}
	if err := displayQRCode(); err != nil {
		return nil, err
	}
	if err := c.createSessionResponse(); err != nil {
		return nil, err
	}
	if !c.wallet.approved {
		return c.wallet, nil
	}
	if err := c.signMessageRequest(); err != nil {
		return nil, err
	}
	if err := c.checkSignMessageResponse(); err != nil {
		return nil, err
	}
	return c.wallet, nil
}

func (c *client) dialWS(ctx context.Context) error {
	wsURL := wallectconnect.GetWebSocketUrl(c.bridgeURL, "wc", "1")
	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return errors.WrapAndReport(err, "dial to wallet connect bridge url")
	}
	c.conn = conn
	return nil
}

func (c *client) close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

func (c *client) sendRequest(payload []byte) error {
	err := c.conn.WriteMessage(websocket.TextMessage, payload)
	if err != nil {
		return errors.WrapAndReport(err, "write wallet connect message to server")
	}
	return nil
}

func (c *client) encryptJSONRpc(jsonRpc string) (*wcMessagePayload, error) {
	iv, err := wallectconnect.GenerateRandomBytes(128 / 8)
	if err != nil {
		return nil, errors.WrapAndReport(err, "generate random bytes")
	}
	data, err := wallectconnect.Aes256Encrypt([]byte(jsonRpc), c.encryptionKey, iv)
	if err != nil {
		return nil, err
	}
	unsigned := append(data, iv...)
	hmac := wallectconnect.HmacSha256(unsigned, c.encryptionKey)
	msg := &wcMessagePayload{
		Data: hex.EncodeToString(data),
		IV:   hex.EncodeToString(iv),
		Hmac: hex.EncodeToString(hmac),
	}
	return msg, nil
}

func (c *client) decryptJSONRpc(msg *wcMessage) (string, error) {
	mp, err := newWCMessagePayloadFromBytes([]byte(msg.Payload))
	if err != nil {
		return "", err
	}
	iv, err := hex.DecodeString(mp.IV)
	if err != nil {
		return "", errors.WrapAndReport(err, "decode iv hex")
	}
	cipher, err := hex.DecodeString(mp.Data)
	if err != nil {
		return "", errors.WrapAndReport(err, "decode cipher hex")
	}
	// 校验hmac一致性
	unsigned := append(cipher, iv...)
	hmac := wallectconnect.HmacSha256(unsigned, c.encryptionKey)
	hmacHex := hex.EncodeToString(hmac)
	if hmacHex != mp.Hmac {
		return "", errors.NewWithReport("inconsistent session message hmac")
	}
	// 解密数据
	data, err := wallectconnect.Aes256Decrypt(cipher, c.encryptionKey, iv)
	if err != nil {
		return "", errors.WrapAndReport(err, "aes256 decrypt")
	}
	return string(data), nil
}

func (c *client) readWalletConnectResponse() (string, error) {
	if err := c.conn.SetReadDeadline(time.Now().Add(c.readTimeout)); err != nil {
		return "", errors.WrapAndReport(err, "set websocket read timeout")
	}
	msgType, data, err := c.conn.ReadMessage()
	if nil != err {
		return "", errors.WrapAndReport(err, "read session response")
	}
	switch msgType {
	case websocket.TextMessage:
		log.Debugf("wallet connect - receive:%v", string(data))
		if err := c.sessionMessageACK(); err != nil {
			return "", err
		}
		msg, err := newWCMessageFromBytes(data)
		if err != nil {
			return "", err
		}
		payload, err := c.decryptJSONRpc(msg)
		if err != nil {
			return "", err
		}
		if sessionClosed := c.checkSessionUpdate(payload); sessionClosed {
			return "", errSessionClosed
		}
		return payload, nil
	case websocket.CloseMessage: //关闭
		return "", errSessionClosed
	default:
		return "", errors.NewWithReport("unsupported message type")
	}
}

func (c *client) checkSessionUpdate(jsonRpc string) (sessionClosed bool) {
	// 检查是否是会话更新或者链接断开
	method := gjson.Get(jsonRpc, "method").String()
	if method != "wc_sessionUpdate" {
		return false
	}
	params := gjson.Get(jsonRpc, "params").Array()
	if len(params) == 0 {
		// 不应该发生
		return false
	}
	approved := params[0].Get("approved")
	if !approved.Exists() {
		// 不应该发生
		return false
	}
	if approved.Bool() {
		return false
	}
	// 用户断开链接
	log.Warnf("wallet connect - session closed from request %v", jsonRpc)
	return true
}

func (c *client) sessionMessageACK() error {
	msg := wcMessage{
		Topic:   c.clientID,
		Type:    "ack",
		Payload: "",
		Silent:  true,
	}
	log.Debugf("wallet connect - session message ack:%v", string(msg.Marshal()))
	return c.sendRequest(msg.Marshal())
}

func (c *client) createSessionRequest() error {
	jsonRpc := newJSONRpcRequest("wc_sessionRequest", peer{
		PeerID: c.clientID,
		PeerMeta: clientMeta{
			Description: "test for discord qr image to connect wallet",
			Name:        "test sdk",
		},
	})
	payload, err := c.encryptJSONRpc(jsonRpc.Marshal())
	if err != nil {
		return err
	}
	msg := wcMessage{
		Topic:   c.handshakeTopic,
		Type:    "pub",
		Payload: payload.Marshal(),
		Silent:  true,
	}
	log.Debugf("wallet connect - create session request:%v", string(msg.Marshal()))
	return c.sendRequest(msg.Marshal())
}

func (c *client) subscribeSession() error {
	msg := wcMessage{
		Topic:   c.clientID,
		Type:    "sub",
		Payload: "",
		Silent:  true,
	}
	log.Debugf("wallet connect - subscribe session:%v", string(msg.Marshal()))
	return c.sendRequest(msg.Marshal())
}

func (c *client) createSessionResponse() error {
	sessionResult, err := c.readWalletConnectResponse()
	if err != nil {
		if errors.Is(err, errSessionClosed) {
			return nil
		}
		return err
	}
	log.Debugf("wallet connect - create session response:%v", sessionResult)
	errStr := gjson.Get(sessionResult, "error").String()
	if errStr != "" {
		if strings.Contains(errStr, "Session Rejected") {
			return nil
		}
		return errors.New(errStr)
	}
	result := gjson.Get(sessionResult, "result").String()
	if err := json.Unmarshal([]byte(result), &c.wallet); err != nil {
		return errors.WrapAndReport(err, "unmarshal wallet info")
	}
	c.wallet.approved = true
	if len(c.wallet.Accounts) == 0 {
		return errors.NewWithReport("no wallet accounts acquired")
	}
	return nil
}

func (c *client) signMessageRequest() error {
	jsonRpc := newJSONRpcRequest("eth_sign", c.wallet.Accounts[0], c.signMsg)
	payload, err := c.encryptJSONRpc(jsonRpc.Marshal())
	if err != nil {
		return err
	}
	msg := wcMessage{
		Topic:   c.wallet.PeerID,
		Type:    "pub",
		Payload: payload.Marshal(),
		Silent:  true,
	}
	log.Debugf("wallet connect - sign message request:%v", string(msg.Marshal()))
	return c.sendRequest(msg.Marshal())
}

func (c *client) checkSignMessageResponse() error {
	signResult, err := c.readWalletConnectResponse()
	if err != nil {
		if errors.Is(err, errSessionClosed) {
			return nil
		}
		return err
	}
	log.Debugf("wallet connect - sign message response:%v", signResult)
	signatureHex := gjson.Get(signResult, "result").String()
	c.wallet.signed = c.verifyEthSignature(c.wallet.Accounts[0], signatureHex, []byte(c.signMsg))
	return nil
}

func (c *client) verifyEthSignature(signAddrHex, signatureHex string, msg []byte) bool {
	sig, err := hexutil.Decode(signatureHex)
	if err != nil {
		return false
	}
	msg = accounts.TextHash(msg)
	sig[crypto.RecoveryIDOffset] -= 27 // Transform yellow paper V from 27/28 to 0/1
	recovered, err := crypto.SigToPub(msg, sig)
	if err != nil {
		return false
	}
	recoveredAddr := crypto.PubkeyToAddress(*recovered)
	return signAddrHex == recoveredAddr.Hex()
}
