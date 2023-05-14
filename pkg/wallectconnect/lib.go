package wallectconnect

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)


// 第一步：建立链接
// 	存在URI的情况下，订阅会话请求
// 第二步：对方链接未建立的情况下createSession(): 构建jsonrpc的请求，加密，this._transport.send(payload, topic, silent);
//		加密规则:https://github.com/WalletConnect/walletconnect-monorepo/blob/6d440e7990ecfab3b1dca10a8ff45f72af0e1541/legacy/client/src/crypto.ts#L39
//		加密后json序列化
// 第三步：订阅connect\session_update\disconnect事件
func extractHostname(url string) string {
	var hostname string
	idx := strings.Index(url, "//")
	if idx > -1 {
		hostname = strings.Split(url, "/")[2]
	} else {
		hostname = strings.Split(url, "/")[1]
	}
	hostname = strings.Split(hostname, ":")[0]
	return strings.Split(hostname, "?")[0]
}

func ExtractRootDomain(url string) string {
	hostname := extractHostname(url)
	arr := strings.Split(hostname, ".")
	arr = arr[len(arr)-2:]
	return strings.Join(arr, ".")
}

const (
	alphanumerical  = "abcdefghijklmnopqrstuvwxyz0123456789"
	bridgeURLFormat = "https://%v.bridge.walletconnect.org"
)



func RandomBridgeURL() string {
	rand.Seed(time.Now().Unix())
	n := rand.Intn(36)
	c := alphanumerical[n]
	return fmt.Sprintf(bridgeURLFormat, string(c))
}

func GetWebSocketUrl(url ,protocol,version string) string  {
	if strings.HasPrefix(url,"https") {
		url = strings.Replace(url, "https","wss", 1)
	}
	return url+"?protocol="+protocol+"&version="+version+"&env=BotInfo"
}