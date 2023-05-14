package walletconnect

import (
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

func verifySignature(signAddrHex, signatureHex string, msg []byte) bool {
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
