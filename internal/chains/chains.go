package chains

type Blockchain struct {
	ID    int
	IDHex string
	Name  string
}

var (
	// moralis文档中拷贝
	Array = []*Blockchain{
		{
			ID:    1,
			IDHex: "0x1",
			Name:  "eth",
		},
		{
			ID:    3,
			IDHex: "0x3",
			Name:  "ropsten",
		},
		{
			ID:   4,
			Name: "rinkeby",
		},
		{
			ID:    5,
			IDHex: "0x5",
			Name:  "goerli",
		},
		{
			ID:    42,
			IDHex: "0x2a",
			Name:  "kovan",
		},
		{
			ID:    137,
			IDHex: "0x89",
			Name:  "polygon",
		},
		{
			ID:    80001,
			IDHex: "0x13881",
			Name:  "mumbai",
		},
		{
			ID:    56,
			IDHex: "0x38",
			Name:  "bsc",
		},
		{
			ID:    97,
			IDHex: "0x61",
			Name:  "bsc testnet",
		},
		{
			ID:    43114,
			IDHex: "0xa86a",
			Name:  "avalanche",
		},
		{
			ID:    43113,
			IDHex: "0xa869",
			Name:  "avalanche testnet",
		},
		{
			ID:    250,
			IDHex: "0xfa",
			Name:  "fantom",
		},
		{
			ID:    25,
			IDHex: "0x19",
			Name:  "cronos",
		},
	}

	Mapping = map[int]*Blockchain{
		1: {
			ID:    1,
			IDHex: "0x1",
			Name:  "eth",
		},
		3: {
			ID:    3,
			IDHex: "0x3",
			Name:  "ropsten",
		},
		4: {
			ID:   4,
			IDHex: "0x4",
			Name: "rinkeby",
		},
		5: {
			ID:    5,
			IDHex: "0x5",
			Name:  "goerli",
		},
		42: {
			ID:    42,
			IDHex: "0x2a",
			Name:  "kovan",
		},
		137: {
			ID:    137,
			IDHex: "0x89",
			Name:  "polygon",
		},
		80001: {
			ID:    80001,
			IDHex: "0x13881",
			Name:  "mumbai",
		},
		56: {
			ID:    56,
			IDHex: "0x38",
			Name:  "bsc",
		},
		97: {
			ID:    97,
			IDHex: "0x61",
			Name:  "bsc testnet",
		},
		43114: {
			ID:    43114,
			IDHex: "0xa86a",
			Name:  "avalanche",
		},
		43113: {
			ID:    43113,
			IDHex: "0xa869",
			Name:  "avalanche testnet",
		},
		250: {
			ID:    250,
			IDHex: "0xfa",
			Name:  "fantom",
		},
		25: {
			ID:    25,
			IDHex: "0x19",
			Name:  "cronos",
		},
	}
)
