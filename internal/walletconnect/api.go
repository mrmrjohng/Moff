package walletconnect

import "context"

// ClientV1 wallet connect交互协议v1版客户端
// 交互流程见文档：https://docs.walletconnect.com/tech-spec#establishing-connection
type ClientV1 interface {

	// GetQRCode 返回钱包连接的二维码，用以展示给交互的用户
	GetQRCode() ([]byte, error)

	// ConnectWallet 在展示二维码后，通过wallet connect v1协议与用户交互，展示消息让用户签名，
	// 获取用户的钱包信息后，校验用户的签名，签名通过与否均会返回 Wallet 对象。
	// Wallet对象： Wallet.Confirmed()
	// 		为true,表示用户建立会话、并且签名通过，可以使用钱包中的地址与chain id.
	//		为false时，需要检查 Wallet.Approved() 返回用户是否建立会话，检查 Wallet.Signed() 返回用户是否同意签名
	ConnectWallet(ctx context.Context, signMsg string, displayQRCode DisplayQRCodeFn) (*Wallet, error)
}

// DisplayQRCodeFn 展示二维码的函数
type DisplayQRCodeFn func() error
