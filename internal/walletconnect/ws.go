package walletconnect

import (
	"encoding/hex"
	"fmt"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/skip2/go-qrcode"
	"moff.io/moff-social/pkg/log"
	"moff.io/moff-social/pkg/wallectconnect"
	"net/url"
	"time"
)

func WsTestGorilla() {
	bridgeURL := wallectconnect.RandomBridgeURL()
	wsURL := wallectconnect.GetWebSocketUrl(bridgeURL, "wc", "1")

	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if nil != err {
		log.Fatal(err)
		return
	}
	defer conn.Close()

	go func() {
		log.Infof("reading server message")
		for {
			msgType, data, err := conn.ReadMessage()
			if nil != err {
				log.Errorf("read server text:%v", err)
				continue
			}
			switch msgType {
			case websocket.TextMessage:

				fmt.Println("receive text:", string(data))
			case websocket.BinaryMessage:
				fmt.Println("receive binary:", data)
			case websocket.CloseMessage: //关闭
				log.Info("receive closing ...")
			default:
				fmt.Println("default:", string(data))
			}
		}
	}()

	key := generateQRCode(topic, bridgeURL)
	createSessionRequest(conn, topic, clientID, key)
	sendSubscribeRequest(conn, clientID)
	select {}
}
func generateQRCode(handshakeTopic, bridgeURL string) []byte {
	keyBytes, _ := wallectconnect.GenerateRandomBytes(256 / 8)
	uri := "wc:" + handshakeTopic + "@1?bridge=" + url.QueryEscape(bridgeURL) + "&key=" + hex.EncodeToString(keyBytes)
	println("uri:", uri)
	err := qrcode.WriteFile(uri, qrcode.Medium, 256, "wallet_connect_qr.png")
	if nil != err {
		log.Fatal(err)
		return nil
	}
	return keyBytes
}

var (
	topic    = uuid.NewString()
	clientID = uuid.NewString()
)

func sendSubscribeRequest(conn *websocket.Conn, peerID string) {
	msg := wcMessage{
		Topic:   peerID,
		Type:    "sub",
		Payload: "",
		Silent:  true,
	}
	payload := msg.Marshal()
	println("create session request:", string(payload))
	err := conn.WriteMessage(websocket.TextMessage, payload)
	if err != nil {
		println("send sub msg error:", err)
	}
}

func createSessionRequest(conn *websocket.Conn, handshakeTopic, peerID string, key []byte) {
	request := newJSONRpcRequest("wc_sessionRequest", peer{
		PeerID: peerID,
		PeerMeta: clientMeta{
			Description: "test for discord qr image to connect wallet",
			Name:        "test sdk",
		},
	}).Marshal()
	iv, err := wallectconnect.GenerateRandomBytes(128 / 8)
	if err != nil {
		panic(err)
	}
	data, err := wallectconnect.Aes256Encrypt([]byte(request), key, iv)
	if err != nil {
		panic(err)
	}
	unsigned := append(data, iv...)
	hmac := wallectconnect.HmacSha256(unsigned, key)
	encryption := wcMessagePayload{
		Data: hex.EncodeToString(data),
		IV:   hex.EncodeToString(iv),
		Hmac: hex.EncodeToString(hmac),
	}
	msg := wcMessage{
		Topic:   handshakeTopic,
		Type:    "pub",
		Payload: encryption.Marshal(),
		Silent:  true,
	}
	payload := msg.Marshal()
	println("create session request:", string(payload))
	err = conn.WriteMessage(websocket.TextMessage, payload)
	if err != nil {
		println("send session request msg error:", err)
	}
}

var (
	// 每一个session的id一致
	globalPayloadID int64
)

func payloadID() int64 {
	if globalPayloadID != 0 {
		return globalPayloadID
	}
	return time.Now().UnixNano() / 1000
}
